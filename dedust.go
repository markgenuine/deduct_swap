package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"strings"
	"time"

	"github.com/markgenuine/dedust_swap/structures"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Router struct {
	Api ton.APIClientWrapped
	Ctx context.Context
}

type Pool struct {
	IsStable bool
	Address  *address.Address
	Reserve0 *big.Int
	Reserve1 *big.Int
	Token0   *address.Address
	Token1   *address.Address
	LpFee    *big.Int
}

type Dedust struct {
	Router       *Router
	Wallet       *wallet.Wallet
	Ctx          context.Context
	Api          ton.APIClientWrapped
	DedustRouter *address.Address
	Fees         Fee
}

type Fee struct {
	TxTon      *big.Int
	ForwardTon *big.Int
}

func NewRouter(api ton.APIClientWrapped, ctx context.Context) *Router {
	return &Router{Api: api, Ctx: ctx}
}

func GetRouterAddress() *address.Address {
	return address.MustParseAddr("EQBfBWT7X2BHg9tXAxzhz2aKiNTU1tpt5NsiK0uSDW_YAJ67")
}

func TonNative() *address.Address {
	return address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")
}

func NewDedust(phrase string) *Dedust {
	client := liteclient.NewConnectionPool()

	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton.org/global.config.json")
	if err != nil {
		log.Fatalln("get config err: ", err.Error())
		return nil
	}

	err = client.AddConnectionsFromConfig(context.Background(), cfg)
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return nil
	}

	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()
	api.SetTrustedBlockFromConfig(cfg)

	ctx := client.StickyContext(context.Background())
	w, err := wallet.FromSeed(api, strings.Split(phrase, " "), wallet.V4R2)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return nil
	}

	return &Dedust{
		Router:       NewRouter(api, ctx),
		Wallet:       w,
		Ctx:          ctx,
		Api:          api,
		DedustRouter: GetRouterAddress(),
		Fees: Fee{
			TxTon:      tlb.MustFromTON("0.3").Nano(),
			ForwardTon: tlb.MustFromTON("0.25").Nano(),
		},
	}
}

func (d *Dedust) commonSwap(tokenIn *address.Address, tokenOut *address.Address, amount tlb.Coins) error {
	err := errors.New("invalid swap strategy")

	nativeTon := TonNative().String()
	if tokenIn.String() == nativeTon && tokenOut.String() != nativeTon {
		err = d.swapTonToJetton(tokenOut, amount)
	} else if tokenIn.String() != nativeTon && tokenOut.String() == nativeTon {
		err = d.swapJettonToTon(tokenIn, amount)
	}

	return err
}

func (d *Dedust) swapJettonToTon(tokenIn *address.Address, amount tlb.Coins) error {
	pool, err := d.GetDedustPool(tokenIn, TonNative())
	if err != nil {
		fmt.Println("Error: ", err)
		return err
	}
	clientWallet := jetton.NewJettonMasterClient(d.Api, tokenIn)
	jWallet, err := clientWallet.GetJettonWallet(d.Ctx, d.Wallet.WalletAddress())
	if err != nil {
		return err
	}

	dedustJSwap := structures.RequestJettonSwap{
		SwapStep: structures.SwapStep{
			PoolAddr: pool.Address,
			SwapStepParams: structures.SwapStepParams{
				Limit: tlb.MustFromTON("0"), //TODO slippage
				Next:  nil,
			},
		},
		SwapParams: structures.SwapParams{
			Deadline:        uint32(time.Now().Unix() + 3600), //TODO deadlibe
			RecipientAddr:   d.Wallet.WalletAddress(),
			ReferralAddr:    address.NewAddressNone(),
			FullfillPayload: nil,
			RejectPayload:   nil,
		},
	}

	dedustJSwapBody, err := tlb.ToCell(&dedustJSwap)
	if err != nil {
		return err
	}

	vaultAddress := d.GetVaultAddress(tokenIn)

	transferRequest := structures.JettonTrasfer{
		QueryId:             rand.Uint64(),
		Amount:              amount,
		Destination:         vaultAddress,
		ResponseDestination: d.Wallet.WalletAddress(),
		CustomPayload:       nil,
		FwdTonAmount:        tlb.FromNanoTON(d.Fees.ForwardTon),
		FwdPayload:          dedustJSwapBody,
	}

	transferRequestBody, err := tlb.ToCell(&transferRequest)
	if err != nil {
		return err
	}

	if err := d.Wallet.Send(
		d.Ctx,
		wallet.SimpleMessage(
			jWallet.Address(),
			tlb.FromNanoTON(d.Fees.TxTon),
			transferRequestBody,
		), true,
	); err != nil {
		return err
	}

	return nil
}

func (d *Dedust) swapTonToJetton(tokenOut *address.Address, amount tlb.Coins) error {
	pool, err := d.GetDedustPool(TonNative(), tokenOut)
	if err != nil {
		fmt.Println("Error: ", err)
		return err
	}

	dedustSwap := structures.RequestNativeSwap{
		QueryID: rand.Uint64(),
		Amount:  amount,
		SwapStep: structures.SwapStep{
			PoolAddr: pool.Address,
			SwapStepParams: structures.SwapStepParams{
				Limit: tlb.MustFromTON("0"), //TODO
				Next:  nil,
			},
		},
		SwapParams: structures.SwapParams{
			Deadline:        uint32(time.Now().Unix()) + 60*60, //TODO check deadlibe
			RecipientAddr:   d.Wallet.WalletAddress(),
			ReferralAddr:    address.NewAddressNone(),
			FullfillPayload: nil,
			RejectPayload:   nil,
		},
	}

	vaultAddress := d.GetVaultAddress(TonNative())

	swapBody, err := tlb.ToCell(&dedustSwap)
	if err != nil {
		fmt.Println("Error while converting TON coin to cell ", err)
		return err
	}

	message := wallet.SimpleMessage(
		vaultAddress,
		tlb.FromNanoTON(new(big.Int).Add(amount.Nano(), d.Fees.TxTon)),
		swapBody,
	)

	//if err := d.Wallet.Send(
	//	d.Ctx,
	//	message, true,
	//); err != nil {
	//	fmt.Println("Error: ", err)
	//	return err
	//}

	transactions := make(chan *tlb.Transaction)

	master, err := d.Api.CurrentMasterchainInfo(d.Ctx)
	if err != nil {
		fmt.Println("get masterchain info err: ", err.Error())
		return err
	}

	acc, err := d.Api.GetAccount(context.Background(), master, d.DedustRouter)
	if err != nil {
		fmt.Println("get masterchain info err: ", err.Error())
		return err
	}

	tx, _, err := d.Wallet.SendWaitTransaction(d.Ctx, message)
	if err != nil {
		fmt.Println("Not send in blockchain swap ", err)
		return err
	}

	lastProcessedLT := acc.LastTxLT

	ctxSubscribe, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	go d.Api.SubscribeOnTransactions(ctxSubscribe, d.DedustRouter, lastProcessedLT, transactions)
	for tx := range transactions {

		// process transaction here
		//log.Println(tx.String())

		// update last processed lt and save it in db
		//lastProcessedLT = tx.LT
	}

	return nil
}

func (d *Dedust) GetDedustPool(from *address.Address, to *address.Address) (*Pool, error) {
	b, _ := d.Router.Api.CurrentMasterchainInfo(d.Router.Ctx)

	//stable - 1, volatile - 0
	poolType := 0 //TODO make it or not???
	// add different stable pools
	/*if (from.String() == jetton.JUSDT().Address.String() && to.String() == jetton.JUSDC().Address.String()) ||
		(from.String() == jetton.JUSDC().Address.String() && to.String() == jetton.JUSDT().Address.String()) {
		poolType = 1
	}*/
	result, err := d.Router.Api.RunGetMethod(d.Router.Ctx, b, GetRouterAddress(), "get_pool_address", poolType, createSliceForToken(from), createSliceForToken(to))
	if err != nil {
		fmt.Println("Error getting pool Address: ", err)
		return nil, err
	}
	poolAddress := result.MustSlice(0).MustToCell().BeginParse().MustLoadAddr()

	result, err = d.Router.Api.RunGetMethod(d.Router.Ctx, b, poolAddress, "get_assets")
	if err != nil {
		fmt.Println("Error getting pool assets: ", err)
		return nil, err
	}
	assetAddress0 := createAddressFromSlice(result.MustSlice(0))
	assetAddress1 := createAddressFromSlice(result.MustSlice(1))

	result, err = d.Router.Api.RunGetMethod(d.Router.Ctx, b, poolAddress, "get_reserves")
	if err != nil {
		fmt.Println("Error getting pool reserves: ", err)
		return nil, err
	}

	reserves := result.AsTuple()
	reserve0 := reserves[0].(*big.Int)
	reserve1 := reserves[1].(*big.Int)

	isStable := false
	//if poolType == 1 {
	//	isStable = true
	//}
	return &Pool{
		IsStable: isStable,
		Address:  poolAddress,
		Reserve0: reserve0,
		Reserve1: reserve1,
		Token0:   assetAddress0,
		Token1:   assetAddress1,
		LpFee:    big.NewInt(0),
	}, nil
}

func (d *Dedust) GetVaultAddress(token *address.Address) *address.Address {
	b, _ := d.Api.CurrentMasterchainInfo(d.Ctx)
	result, err := d.Router.Api.RunGetMethod(d.Ctx, b, GetRouterAddress(), "get_vault_address", createSliceForToken(token))
	if err != nil {
		fmt.Println("Error getting pool reserves: ", err)
		return nil
	}

	return result.MustSlice(0).MustLoadAddr()
}

func createAddressFromSlice(slice *cell.Slice) *address.Address {
	if slice.BitsLeft() == 4 {
		return address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")
	} else {
		slice.MustLoadUInt(4)
		slice.MustLoadUInt(8)
		return address.NewAddress(0, 0, slice.MustLoadBinarySnake())
	}
}

func createSliceForToken(tokenAddr *address.Address) *cell.Slice {
	if tokenAddr.String() == "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c" {
		return cell.BeginCell().
			MustStoreUInt(0, 4).
			EndCell().BeginParse()
	} else {
		return cell.BeginCell().
			// type (0 native, 1 jetton)
			MustStoreUInt(1, 4).
			// workchain
			MustStoreUInt(0, 8).
			MustStoreBinarySnake(tokenAddr.Data()).
			EndCell().BeginParse()
	}
}

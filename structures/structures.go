package structures

import (
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type JettonTrasfer struct {
	_                   tlb.Magic        `tlb:"#0f8a7ea5"`
	QueryId             uint64           `tlb:"## 64"`
	Amount              tlb.Coins        `tlb:"."`
	Destination         *address.Address `tlb:"addr"`
	ResponseDestination *address.Address `tlb:"addr"`
	CustomPayload       *cell.Cell       `tlb:"maybe ^"`
	FwdTonAmount        tlb.Coins        `tlb:"."`
	FwdPayload          *cell.Cell       `tlb:"either . ^"`
}

type SwapStepParams struct {
	_     tlb.Magic `tlb:"$0"`
	Limit tlb.Coins `tlb:"."`
	Next  *SwapStep `tlb:"maybe ^"`
}

type SwapStep struct {
	PoolAddr       *address.Address `tlb:"addr"`
	SwapStepParams `tlb:"."`
}

type SwapParams struct {
	Deadline        uint32           `tlb:"## 32"`
	RecipientAddr   *address.Address `tlb:"addr"`
	ReferralAddr    *address.Address `tlb:"addr"`
	FullfillPayload *cell.Cell       `tlb:"maybe ^"`
	RejectPayload   *cell.Cell       `tlb:"maybe ^"`
}

type RequestNativeSwap struct {
	_          tlb.Magic `tlb:"#ea06185d"`
	QueryID    uint64    `tlb:"## 64"`
	Amount     tlb.Coins `tlb:"."`
	SwapStep   `tlb:"."`
	SwapParams `tlb:"^"`
}

type RequestJettonSwap struct {
	_          tlb.Magic `tlb:"#e3a0d482"`
	SwapStep   `tlb:"."`
	SwapParams `tlb:"^"`
}

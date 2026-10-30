package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

// redeemable reports whether a market pays out now: resolved (YES/NO pay
// winning shares 1:1) or cancelled / INVALID (every share pays at the
// settlement price), and past its redemption delay.
func (e *simEnv) redeemable(m types.Market) bool {
	switch m.Status {
	case "RESOLVED_YES", "RESOLVED_NO", "CANCELLED", "RESOLVED_INVALID":
	default:
		return false
	}
	return m.RedemptionBlocks <= 0 || e.ctx.BlockHeight() >= m.ResolutionHeight+m.RedemptionBlocks
}

// redeemShares returns the share coins acc would redeem on m (the coins the
// handler takes): the winning side for YES/NO, both sides otherwise.
func (e *simEnv) redeemShares(acc simtypes.Account, m types.Market) sdk.Coins {
	var outcomes []string
	switch m.Status {
	case "RESOLVED_YES":
		outcomes = []string{"yes"}
	case "RESOLVED_NO":
		outcomes = []string{"no"}
	default:
		outcomes = []string{"yes", "no"}
	}
	coins := sdk.NewCoins()
	for _, o := range outcomes {
		if bal := e.bk.GetBalance(e.ctx, acc.Address, shareDenom(m.Index, o)); bal.Amount.IsPositive() {
			coins = coins.Add(bal)
		}
	}
	return coins
}

// SimulateMsgRedeem has a live sim holder of payable shares on a resolved
// or cancelled market redeem them. Markets resolve in the EndBlocker at
// their end block, so redemption follows the trades of earlier blocks; the
// shares the handler burns are the tx's CoinsSpentInMsg (the random fee
// never spends them).
func SimulateMsgRedeem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgRedeem{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		payable, err := e.markets(e.redeemable)
		if err != nil {
			return noOpErr(label, err)
		}
		holders := e.live(nil)
		for _, i := range e.r.Perm(len(payable)) {
			m := payable[i]
			for _, j := range e.r.Perm(len(holders)) {
				acc := holders[j]
				shares := e.redeemShares(acc, m)
				if shares.IsZero() {
					continue
				}
				msg := &types.MsgRedeem{Creator: acc.Address.String(), MarketId: m.Index}
				if e.usable(acc, nil) != "" || e.dryRun(msg) != nil {
					continue
				}
				return e.deliver(acc, msg, shares)
			}
		}
		if len(payable) == 0 {
			return noOp(label, "no resolved or cancelled market past its redemption delay")
		}
		return noOp(label, "no live sim account holds payable shares")
	})
}

// SimulateMsgWithdrawLiquidity has the creator of a resolved market withdraw
// the part of its LMSR residual not yet paid out. The EndBlocker refunds
// the residual at resolution, so this succeeds only where that automatic
// refund failed; otherwise the op is a NoOp.
func SimulateMsgWithdrawLiquidity(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgWithdrawLiquidity{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		resolved, err := e.markets(func(m types.Market) bool {
			switch m.Status {
			case "RESOLVED_YES", "RESOLVED_NO", "RESOLVED_INVALID":
				return true
			}
			return false
		})
		if err != nil {
			return noOpErr(label, err)
		}
		mine := 0
		for _, i := range e.r.Perm(len(resolved)) {
			m := resolved[i]
			acc, ok := e.account(m.Creator)
			if !ok {
				continue
			}
			mine++
			msg := &types.MsgWithdrawLiquidity{Creator: m.Creator, MarketId: m.Index}
			if e.dryRun(msg) != nil {
				continue
			}
			return e.deliver(acc, msg, nil)
		}
		if mine == 0 {
			return noOp(label, "no resolved market created by a live sim account")
		}
		return noOp(label, "every residual was already refunded at resolution")
	})
}

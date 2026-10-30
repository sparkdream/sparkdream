package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgSpendFromCommons spends a small share (at most 5%) of a group
// policy's treasury to a live sim account, through a proposal of that
// group. The spender is any body holding MsgSpendFromCommons whose live sim
// seats can pass it (in practice the Commons Council, the Commons
// Operations Committee and simulated groups) and whose treasury, funded by
// x/split, holds SPARK. The handler's activation, term and per-epoch gates
// are checked by the dry-run.
func SimulateMsgSpendFromCommons(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSpendFromCommons{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			denom := e.k.BondDenom(e.ctx)
			groups, err := e.groups(func(string, types.Group) bool { return true })
			if err != nil {
				return proposal{}, "", err
			}
			var funded []string
			for _, g := range groups {
				addr, err := e.ak.AddressCodec().StringToBytes(g.g.PolicyAddress)
				if err != nil {
					continue
				}
				if e.bk.SpendableCoins(e.ctx, addr).AmountOf(denom).GTE(math.NewInt(20)) {
					funded = append(funded, g.g.PolicyAddress)
				}
			}
			if len(funded) == 0 {
				return proposal{}, "no group treasury holds SPARK", nil
			}
			policy, proposer, reason, err := e.pickCarrier(funded, sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			b, _, err := e.policyBody(policy)
			if err != nil {
				return proposal{}, "", err
			}
			addr, err := e.ak.AddressCodec().StringToBytes(policy)
			if err != nil {
				return proposal{}, "", err
			}
			maxAmt := e.bk.SpendableCoins(e.ctx, addr).AmountOf(denom).QuoRaw(20)
			if c := b.g.MaxSpendPerEpoch; c != nil && c.IsPositive() && maxAmt.GT(*c) {
				maxAmt = *c
			}
			amt, err := simtypes.RandPositiveInt(e.r, maxAmt)
			if err != nil {
				return proposal{}, "treasury too small to spend from", nil
			}
			recipient, ok := pick(e.r, e.liveAccounts())
			if !ok {
				return proposal{}, "no live sim account to receive the spend", nil
			}
			return proposal{policy: policy, proposer: proposer, inner: &types.MsgSpendFromCommons{
				Authority: policy,
				Recipient: recipient.Address.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin(denom, amt)),
			}}, "", nil
		})
	})
}

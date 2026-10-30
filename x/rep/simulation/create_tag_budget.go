package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCreateTagBudget has a group policy escrow SPARK as a tag budget,
// through an executed x/commons proposal of a body whose allowlist carries
// the message (council.go), one step per call. When the policy cannot fund
// the pool, one of its seats sends it the SPARK first (a real bank send).
func SimulateMsgCreateTagBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCreateTagBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgCreateTagBudget")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgCreateTagBudget { return &types.MsgCreateTagBudget{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				for _, m := range taken {
					if t, ok := m.(*types.MsgCreateTagBudget); ok && t.Creator == policy {
						return nil, "a budget proposal of this body is already open", nil
					}
				}
				tag, err := e.budgetTag(policy)
				if err != nil {
					return nil, "", err
				}
				if tag == "" {
					return nil, "every tag already has an active budget of this body", nil
				}
				amount := math.NewInt(int64(5+e.r.Intn(46)) * 1_000_000) // 5-50 SPARK
				if why, err := e.fundPolicy(policy, amount); err != nil || why != "" {
					return nil, why, err
				}
				return &types.MsgCreateTagBudget{
					Creator: policy, Tag: tag, InitialPool: amount.String(), MembersOnly: e.r.Intn(3) == 0,
				}, "", nil
			})
	})
}

// fundPolicy makes sure policy holds amount SPARK, having one of its live
// seats send the shortfall (a real MsgSend) when it does not.
func (e *simEnv) fundPolicy(policy string, amount math.Int) (string, error) {
	have := e.bk.SpendableCoins(e.ctx, accAddr(policy)).AmountOf(e.denom())
	if have.GTE(amount) {
		return "", nil
	}
	short := amount.Sub(have)
	g, ok := e.groupByPolicy(policy)
	if !ok {
		return "policy is not a known group", nil
	}
	for _, acc := range e.liveSeats(g.Index) {
		if e.spendable(acc).LTE(short.MulRaw(3)) {
			continue
		}
		send := &banktypes.MsgSend{
			FromAddress: acc.Address.String(),
			ToAddress:   policy,
			Amount:      sdk.NewCoins(sdk.NewCoin(e.denom(), short)),
		}
		ok, err := e.ensure(acc, send, short)
		if err != nil || ok {
			if !ok {
				return "the policy could not be funded", err
			}
			return "", err
		}
	}
	return "no seat can fund the policy", nil
}

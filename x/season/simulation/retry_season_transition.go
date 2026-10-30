package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgRetrySeasonTransition retries a season transition stuck in
// recovery mode through an executed x/commons proposal (council.go).
// Without a transition in recovery it is a NoOp (see activeTransition).
func SimulateMsgRetrySeasonTransition(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgRetrySeasonTransition{}, decodeAs[types.MsgRetrySeasonTransition](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "a retry is already proposed", nil
			}
			if _, reason, err := activeTransition(e.ctx, e.k); err != nil || reason != "" {
				return nil, reason, err
			}
			recovering, err := inRecovery(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			if !recovering {
				return nil, "transition is not in recovery mode", nil
			}
			return &types.MsgRetrySeasonTransition{Authority: policy}, "", nil
		})
}

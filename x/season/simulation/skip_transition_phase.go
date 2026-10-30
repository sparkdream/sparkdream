package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgSkipTransitionPhase skips a stuck, non-critical phase of an
// in-flight season transition through an executed x/commons proposal
// (council.go), only while the transition is in recovery mode (an operator
// skips a phase that failed, not a healthy one). Without such a transition
// it is a NoOp (see activeTransition); the handler's dry-run refuses the
// critical reputation phases.
func SimulateMsgSkipTransitionPhase(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgSkipTransitionPhase{}, decodeAs[types.MsgSkipTransitionPhase](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "a skip is already proposed", nil
			}
			if _, reason, err := activeTransition(e.ctx, e.k); err != nil || reason != "" {
				return nil, reason, err
			}
			recovering, err := inRecovery(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			if !recovering {
				return nil, "transition phase is not stuck (not in recovery mode)", nil
			}
			return &types.MsgSkipTransitionPhase{Authority: policy}, "", nil
		})
}

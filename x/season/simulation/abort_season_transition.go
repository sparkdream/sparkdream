package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgAbortSeasonTransition aborts an in-flight season transition
// through an executed x/commons proposal (council.go) while it is in the
// only abortable phase (RETRO_REWARDS). Without a transition in flight it
// is a NoOp (see activeTransition).
func SimulateMsgAbortSeasonTransition(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgAbortSeasonTransition{}, decodeAs[types.MsgAbortSeasonTransition](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "an abort is already proposed", nil
			}
			state, reason, err := activeTransition(e.ctx, e.k)
			if err != nil || reason != "" {
				return nil, reason, err
			}
			if state.Phase != types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS {
				return nil, "transition is past the abortable phase: " + state.Phase.String(), nil
			}
			return &types.MsgAbortSeasonTransition{Authority: policy}, "", nil
		})
}

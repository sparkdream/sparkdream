package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgAppealDisplayNameModeration delivers
// MsgAppealDisplayNameModeration from a sim account whose display name is
// under active, unappealed moderation inside the appeal period. Skips
// appellants whose DREAM stake cannot be locked. Moderation cases come from
// the ReportDisplayName operation.
func SimulateMsgAppealDisplayNameModeration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAppealDisplayNameModeration{AppealReason: "simulation appeal"}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		appellants := accountsWhere(accs, func(a simtypes.Account) bool {
			m, err := k.DisplayNameModeration.Get(ctx, a.Address.String())
			return err == nil && m.Active && m.AppealChallengeId == "" &&
				ctx.BlockHeight() <= m.ModeratedAt+commontypes.Saturate[int64](params.DisplayNameAppealPeriodBlocks)
		})
		appellant, ok := pick(r, appellants)
		if !ok {
			return noOp(msg, "no appealable display name moderation")
		}
		msg.Creator = appellant.Address.String()
		if err := dryRunMsg(app, ctx, msg); err != nil {
			return noOp(msg, "handler would reject: "+err.Error())
		}

		return deliver(r, app, ctx, txGen, ak, bk, appellant, msg)
	}
}

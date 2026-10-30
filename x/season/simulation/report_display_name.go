package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgReportDisplayName delivers MsgReportDisplayName from a random
// x/rep member against another account's display name. Skips self-reports,
// targets without a display name or already under moderation, and
// reporters whose DREAM stake cannot be locked.
func SimulateMsgReportDisplayName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgReportDisplayName{Reason: "simulation report"}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		// The report stake is locked DREAM, so the reporter is an x/rep
		// member holding it.
		reporter, ok := pick(r, accountsWhere(accs, func(a simtypes.Account) bool {
			return hasDream(ctx, k, a.Address.String(), params.DisplayNameReportStakeDream.Uint64())
		}))
		if !ok {
			return noOp(msg, "no x/rep member holding the report stake")
		}
		target, ok := pick(r, accountsWhere(accs, func(a simtypes.Account) bool {
			p, has := getProfile(ctx, k, a.Address.String())
			return has && p.DisplayName != ""
		}))
		if !ok {
			return noOp(msg, "no display name to report")
		}
		msg.Creator, msg.Target = reporter.Address.String(), target.Address.String()

		if msg.Creator == msg.Target {
			return noOp(msg, "cannot report own display name")
		}
		profile, ok := getProfile(ctx, k, msg.Target)
		if !ok || profile.DisplayName == "" {
			return noOp(msg, "target has no display name")
		}
		if m, err := k.DisplayNameModeration.Get(ctx, msg.Target); err == nil && m.Active {
			return noOp(msg, "target already moderated")
		}
		if err := dryRunMsg(app, ctx, msg); err != nil {
			return noOp(msg, "handler would reject: "+err.Error())
		}

		return deliver(r, app, ctx, txGen, ak, bk, reporter, msg)
	}
}

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

// SimulateMsgSetDisplayTitle delivers MsgSetDisplayTitle from a random
// account (creating its profile first when needed): it displays one of the
// member's unlocked (or archived) titles that still exists, or clears the
// displayed title.
func SimulateMsgSetDisplayTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()
		msg := &types.MsgSetDisplayTitle{Creator: addr}

		if ok, err := ensureProfile(r, app, ctx, txGen, ak, bk, k, simAccount); err != nil || !ok {
			if err != nil {
				return noOpErr(msg, err)
			}
			return noOp(msg, "could not create member profile")
		}
		profile, _ := getProfile(ctx, k, addr)

		var titles []string
		for _, id := range append(append([]string{}, profile.UnlockedTitles...), profile.ArchivedTitles...) {
			if has, err := k.Title.Has(ctx, id); err == nil && has && id != "" {
				titles = append(titles, id)
			}
		}
		// Displaying no title is always allowed; prefer a real title when the
		// member has one.
		if id, ok := pick(r, titles); ok && r.Intn(4) != 0 {
			msg.TitleId = id
		}

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

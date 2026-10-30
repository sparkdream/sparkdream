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

// SimulateMsgSetUsername delivers MsgSetUsername from a random x/rep member
// (creating its profile first when needed). Skips invalid or taken usernames, the change cooldown, and
// accounts whose DREAM burn or x/name reservation would fail.
func SimulateMsgSetUsername(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(&types.MsgSetUsername{}, err)
		}
		// Usernames burn DREAM, so only x/rep members holding the cost can
		// set one.
		simAccount, ok := pick(r, accountsWhere(accs, func(a simtypes.Account) bool {
			return hasDream(ctx, k, a.Address.String(), params.UsernameCostDream.Uint64())
		}))
		if !ok {
			return noOp(&types.MsgSetUsername{}, "no x/rep member holding the username cost")
		}
		addr := simAccount.Address.String()
		msg := &types.MsgSetUsername{Creator: addr, Username: randomUsername(r)}

		if k.ValidateUsername(ctx, msg.Username) != nil {
			return noOp(msg, "invalid username")
		}
		if owner, err := k.UsernameOwner.Get(ctx, msg.Username); err == nil && owner != addr {
			return noOp(msg, "username taken")
		}
		if ok, err := ensureProfile(r, app, ctx, txGen, ak, bk, k, simAccount); err != nil || !ok {
			if err != nil {
				return noOpErr(msg, err)
			}
			return noOp(msg, "could not create member profile")
		}
		profile, _ := getProfile(ctx, k, addr)
		if profile.LastUsernameChangeEpoch > 0 {
			since := k.GetCurrentEpoch(ctx) - profile.LastUsernameChangeEpoch
			if commontypes.Saturate[uint64](since) < params.UsernameChangeCooldownEpochs {
				return noOp(msg, "username change cooldown")
			}
		}
		if err := dryRunMsg(app, ctx, msg); err != nil {
			return noOp(msg, "handler would reject: "+err.Error())
		}

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

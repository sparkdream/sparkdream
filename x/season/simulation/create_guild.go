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

// SimulateMsgCreateGuild delivers MsgCreateGuild from a random x/rep member
// outside any guild (creating its profile first when needed). Skips
// maintenance mode, taken or invalid names, the guild-hop cooldown and
// per-season cap, and accounts whose DREAM burn or x/name reservation of
// the guild name would fail.
func SimulateMsgCreateGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(&types.MsgCreateGuild{}, err)
		}
		// Guild creation burns DREAM, so only x/rep members holding the
		// creation cost and outside a guild can create one.
		simAccount, ok := pick(r, accountsWhere(accs, func(a simtypes.Account) bool {
			p, has := getProfile(ctx, k, a.Address.String())
			return (!has || p.GuildId == 0) && hasDream(ctx, k, a.Address.String(), params.GuildCreationCost.Uint64())
		}))
		if !ok {
			return noOp(&types.MsgCreateGuild{}, "no x/rep member with the creation cost outside a guild")
		}
		addr := simAccount.Address.String()
		msg := &types.MsgCreateGuild{
			Creator:     addr,
			Name:        randomGuildName(r),
			Description: "A simulation generated guild",
			InviteOnly:  r.Intn(2) == 1,
		}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		if k.ValidateGuildName(ctx, msg.Name) != nil {
			return noOp(msg, "guild name invalid or taken")
		}
		if k.ValidateGuildDescription(ctx, msg.Description) != nil {
			return noOp(msg, "invalid guild description")
		}
		if ok, err := ensureProfile(r, app, ctx, txGen, ak, bk, k, simAccount); err != nil || !ok {
			if err != nil {
				return noOpErr(msg, err)
			}
			return noOp(msg, "could not create member profile")
		}
		if reason := joinBlocker(ctx, k, params, addr); reason != "" {
			return noOp(msg, reason)
		}
		if err := dryRunMsg(app, ctx, msg); err != nil {
			return noOp(msg, "handler would reject: "+err.Error())
		}

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

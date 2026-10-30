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

// SimulateMsgJoinGuild delivers MsgJoinGuild from a random account outside
// any guild (creating its profile first when needed) into an active guild
// with room that is open or holds an invite for it. Skips maintenance mode,
// the guild-hop cooldown and the per-season guild cap.
func SimulateMsgJoinGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()
		msg := &types.MsgJoinGuild{Creator: addr}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			if g.Status != types.GuildStatus_GUILD_STATUS_ACTIVE || !guildHasRoom(ctx, k, params, g.Id) {
				return false
			}
			// An invite-only guild needs an unexpired invite.
			return !g.InviteOnly || inviteLive(ctx, k, g.Id, addr)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no joinable guild")
		}
		msg.GuildId = guild.Id

		if p, ok := getProfile(ctx, k, addr); ok && p.GuildId != 0 {
			return noOp(msg, "already in a guild")
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

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

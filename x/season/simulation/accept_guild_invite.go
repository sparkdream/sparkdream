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

// SimulateMsgAcceptGuildInvite delivers MsgAcceptGuildInvite for a random
// unexpired invite held by a sim account, into an active guild with room.
// Skips maintenance mode, invitees already in a guild, the guild-hop
// cooldown and the per-season guild cap. Invites come from the
// InviteToGuild operation.
func SimulateMsgAcceptGuildInvite(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptGuildInvite{}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		epoch := k.GetCurrentEpoch(ctx)
		var invites []types.GuildInvite
		if err := k.GuildInvite.Walk(ctx, nil, func(_ string, inv types.GuildInvite) (bool, error) {
			// An expired invite is removed by the handler, which then fails.
			if inv.ExpiresEpoch == 0 || epoch <= inv.ExpiresEpoch {
				invites = append(invites, inv)
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		invite, ok := pick(r, invites)
		if !ok {
			return noOp(msg, "no unexpired guild invite")
		}
		simAccount, ok := getAccountForAddress(invite.GuildInvitee, accs)
		if !ok {
			return noOp(msg, "invitee is not a sim account")
		}
		msg.Creator, msg.GuildId = invite.GuildInvitee, invite.GuildId

		guild, err := k.Guild.Get(ctx, invite.GuildId)
		if err != nil {
			return noOp(msg, "guild not found")
		}
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return noOp(msg, "guild not active")
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !guildHasRoom(ctx, k, params, guild.Id) {
			return noOp(msg, "guild full")
		}
		if reason := joinBlocker(ctx, k, params, msg.Creator); reason != "" {
			return noOp(msg, reason)
		}

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

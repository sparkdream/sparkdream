package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgInviteToGuild delivers MsgInviteToGuild from the founder or an
// officer of an active guild to a random account outside any guild
// (creating the invitee's profile first when needed). Skips invitees with a
// live invite and guilds at max_pending_invites (after the handler's
// expired-invite pruning).
func SimulateMsgInviteToGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgInviteToGuild{}

		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			return g.Status == types.GuildStatus_GUILD_STATUS_ACTIVE
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no active guild")
		}
		inviters := accountsWhere(accs, func(a simtypes.Account) bool {
			return a.Address.String() == guild.Founder || isOfficer(guild, a.Address.String())
		})
		inviter, ok := pick(r, inviters)
		if !ok {
			return noOp(msg, "guild founder/officers are not sim accounts")
		}
		invitee, _ := simtypes.RandomAcc(r, accs)
		inviteeAddr := invitee.Address.String()
		msg.Creator, msg.GuildId, msg.Invitee = inviter.Address.String(), guild.Id, inviteeAddr

		if p, ok := getProfile(ctx, k, inviteeAddr); ok && p.GuildId != 0 {
			return noOp(msg, "invitee already in a guild")
		}

		// Live pending invites after the handler prunes expired and
		// dangling ones.
		epoch := k.GetCurrentEpoch(ctx)
		live := 0
		for _, a := range guild.PendingInvites {
			inv, err := k.GuildInvite.Get(ctx, fmt.Sprintf("%d:%s", guild.Id, a))
			if err != nil || (inv.ExpiresEpoch > 0 && epoch > inv.ExpiresEpoch) {
				continue
			}
			live++
			if a == inviteeAddr {
				return noOp(msg, "invitee already invited")
			}
		}
		if k.HasPendingGuildInvite(ctx, guild.Id, inviteeAddr) {
			return noOp(msg, "invitee already has an invite record")
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		if commontypes.Saturate[uint32](live) >= params.MaxPendingInvites {
			return noOp(msg, "max pending invites")
		}

		if ok, err := ensureProfile(r, app, ctx, txGen, ak, bk, k, invitee); err != nil || !ok {
			if err != nil {
				return noOpErr(msg, err)
			}
			return noOp(msg, "could not create invitee profile")
		}

		return deliver(r, app, ctx, txGen, ak, bk, inviter, msg)
	}
}

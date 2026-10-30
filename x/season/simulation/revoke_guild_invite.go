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

// SimulateMsgRevokeGuildInvite delivers MsgRevokeGuildInvite for a random
// pending invite, signed by the guild's founder or an officer (revoke) or by
// the invitee (decline). Skips invites whose possible signers are not sim
// accounts.
func SimulateMsgRevokeGuildInvite(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRevokeGuildInvite{}

		var invites []types.GuildInvite
		if err := k.GuildInvite.Walk(ctx, nil, func(_ string, inv types.GuildInvite) (bool, error) {
			invites = append(invites, inv)
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		invite, ok := pick(r, invites)
		if !ok {
			return noOp(msg, "no guild invite")
		}
		msg.GuildId, msg.Invitee = invite.GuildId, invite.GuildInvitee

		guild, err := k.Guild.Get(ctx, invite.GuildId)
		if err != nil {
			return noOp(msg, "guild not found")
		}
		signers := accountsWhere(accs, func(a simtypes.Account) bool {
			s := a.Address.String()
			return s == invite.GuildInvitee || s == guild.Founder || isOfficer(guild, s)
		})
		signer, ok := pick(r, signers)
		if !ok {
			return noOp(msg, "no sim account may revoke or decline this invite")
		}
		msg.Creator = signer.Address.String()

		return deliver(r, app, ctx, txGen, ak, bk, signer, msg)
	}
}

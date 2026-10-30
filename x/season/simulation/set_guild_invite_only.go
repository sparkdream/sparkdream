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

// SimulateMsgSetGuildInviteOnly delivers MsgSetGuildInviteOnly from the
// founder of a live guild, flipping its invite-only flag. Skips guilds
// whose founder is not a sim account.
func SimulateMsgSetGuildInviteOnly(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetGuildInviteOnly{}

		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			return g.Status != types.GuildStatus_GUILD_STATUS_DISSOLVED
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no live guild")
		}
		founder, ok := getAccountForAddress(guild.Founder, accs)
		if !ok {
			return noOp(msg, "guild founder is not a sim account")
		}
		msg.Creator, msg.GuildId, msg.InviteOnly = guild.Founder, guild.Id, !guild.InviteOnly

		return deliver(r, app, ctx, txGen, ak, bk, founder, msg)
	}
}

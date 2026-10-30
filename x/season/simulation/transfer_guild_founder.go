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

// SimulateMsgTransferGuildFounder delivers MsgTransferGuildFounder from the
// founder of a live guild to another active member. Skips guilds whose
// founder is not a sim account or that have no other member.
func SimulateMsgTransferGuildFounder(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgTransferGuildFounder{}

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
		members, err := guildMembers(ctx, k, guild.Id)
		if err != nil {
			return noOpErr(msg, err)
		}
		var candidates []string
		for _, m := range members {
			if m != guild.Founder {
				candidates = append(candidates, m)
			}
		}
		newFounder, ok := pick(r, candidates)
		if !ok {
			return noOp(msg, "no other member to transfer to")
		}
		msg.Creator, msg.GuildId, msg.NewFounder = guild.Founder, guild.Id, newFounder

		return deliver(r, app, ctx, txGen, ak, bk, founder, msg)
	}
}

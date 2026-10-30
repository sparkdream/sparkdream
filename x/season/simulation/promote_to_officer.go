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

// SimulateMsgPromoteToOfficer delivers MsgPromoteToOfficer from the founder
// of an active guild for a member who is not yet an officer. Skips guilds
// whose founder is not a sim account and guilds at max_guild_officers.
func SimulateMsgPromoteToOfficer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgPromoteToOfficer{}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			return g.Status == types.GuildStatus_GUILD_STATUS_ACTIVE && commontypes.Saturate[uint32](len(g.Officers)) < params.MaxGuildOfficers
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no active guild with officer room")
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
			if m != guild.Founder && !isOfficer(guild, m) {
				candidates = append(candidates, m)
			}
		}
		member, ok := pick(r, candidates)
		if !ok {
			return noOp(msg, "no member to promote")
		}
		msg.Creator, msg.GuildId, msg.Member = guild.Founder, guild.Id, member

		return deliver(r, app, ctx, txGen, ak, bk, founder, msg)
	}
}

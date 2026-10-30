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

// SimulateMsgDissolveGuild delivers MsgDissolveGuild from the founder of a
// live guild at least min_guild_age_epochs old. Skips younger guilds (with
// the default epoch length most runs end before any guild is old enough)
// and guilds whose founder is not a sim account.
func SimulateMsgDissolveGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDissolveGuild{}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		epoch := k.GetCurrentEpoch(ctx)
		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			age := epoch - k.BlockToEpoch(ctx, g.CreatedBlock)
			return g.Status != types.GuildStatus_GUILD_STATUS_DISSOLVED && commontypes.Saturate[uint64](age) >= params.MinGuildAgeEpochs
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no live guild old enough to dissolve")
		}
		founder, ok := getAccountForAddress(guild.Founder, accs)
		if !ok {
			return noOp(msg, "guild founder is not a sim account")
		}
		msg.Creator, msg.GuildId = guild.Founder, guild.Id

		return deliver(r, app, ctx, txGen, ak, bk, founder, msg)
	}
}

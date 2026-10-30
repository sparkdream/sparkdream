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

// SimulateMsgDemoteOfficer delivers MsgDemoteOfficer from the founder of a
// live guild for one of its officers. Skips guilds without officers or
// whose founder is not a sim account.
func SimulateMsgDemoteOfficer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDemoteOfficer{}

		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			return g.Status != types.GuildStatus_GUILD_STATUS_DISSOLVED && len(g.Officers) > 0
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no live guild with officers")
		}
		founder, ok := getAccountForAddress(guild.Founder, accs)
		if !ok {
			return noOp(msg, "guild founder is not a sim account")
		}
		officer, _ := pick(r, guild.Officers)
		msg.Creator, msg.GuildId, msg.Officer = guild.Founder, guild.Id, officer

		return deliver(r, app, ctx, txGen, ak, bk, founder, msg)
	}
}

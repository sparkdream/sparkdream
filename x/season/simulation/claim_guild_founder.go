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

// SimulateMsgClaimGuildFounder delivers MsgClaimGuildFounder from an active
// member of a frozen guild (guilds freeze when a leave or kick drops them
// below min_guild_members). Skips when no frozen guild has a sim-account
// member.
func SimulateMsgClaimGuildFounder(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgClaimGuildFounder{}

		guilds, err := guildsWhere(ctx, k, func(g types.Guild) bool {
			return g.Status == types.GuildStatus_GUILD_STATUS_FROZEN
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		guild, ok := pick(r, guilds)
		if !ok {
			return noOp(msg, "no frozen guild")
		}
		members, err := guildMembers(ctx, k, guild.Id)
		if err != nil {
			return noOpErr(msg, err)
		}
		var claimers []simtypes.Account
		for _, m := range members {
			if m == guild.Founder {
				continue // the current founder has nothing to claim
			}
			if acc, ok := getAccountForAddress(m, accs); ok {
				claimers = append(claimers, acc)
			}
		}
		claimer, ok := pick(r, claimers)
		if !ok {
			return noOp(msg, "frozen guild has no sim-account member")
		}
		msg.Creator, msg.GuildId = claimer.Address.String(), guild.Id

		return deliver(r, app, ctx, txGen, ak, bk, claimer, msg)
	}
}

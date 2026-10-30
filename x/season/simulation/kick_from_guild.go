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

// SimulateMsgKickFromGuild delivers MsgKickFromGuild from the founder or an
// officer of a live guild against another active member. Skips the founder
// as a target, self-kicks, and officers kicking officers (founder only).
func SimulateMsgKickFromGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgKickFromGuild{Reason: "simulation kick"}

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
		kicker, ok := pick(r, accountsWhere(accs, func(a simtypes.Account) bool {
			return a.Address.String() == guild.Founder || isOfficer(guild, a.Address.String())
		}))
		if !ok {
			return noOp(msg, "guild founder/officers are not sim accounts")
		}
		kickerAddr := kicker.Address.String()
		members, err := guildMembers(ctx, k, guild.Id)
		if err != nil {
			return noOpErr(msg, err)
		}
		var targets []string
		for _, m := range members {
			if m == guild.Founder || m == kickerAddr {
				continue
			}
			if isOfficer(guild, m) && kickerAddr != guild.Founder {
				continue
			}
			targets = append(targets, m)
		}
		target, ok := pick(r, targets)
		if !ok {
			return noOp(msg, "no kickable member")
		}
		msg.Creator, msg.GuildId, msg.Member = kickerAddr, guild.Id, target

		return deliver(r, app, ctx, txGen, ak, bk, kicker, msg)
	}
}

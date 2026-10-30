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

// SimulateMsgLeaveGuild delivers MsgLeaveGuild from a random sim account
// whose profile is in a guild it did not found. Skips maintenance mode and
// founders (who cannot leave).
func SimulateMsgLeaveGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgLeaveGuild{}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		var leavers []simtypes.Account
		for _, acc := range accs {
			addr := acc.Address.String()
			profile, ok := getProfile(ctx, k, addr)
			if !ok || profile.GuildId == 0 {
				continue
			}
			guild, err := k.Guild.Get(ctx, profile.GuildId)
			if err != nil || guild.Founder == addr {
				continue
			}
			leavers = append(leavers, acc)
		}
		simAccount, ok := pick(r, leavers)
		if !ok {
			return noOp(msg, "no sim account can leave a guild")
		}
		msg.Creator = simAccount.Address.String()

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}

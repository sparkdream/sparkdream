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

// SimulateMsgLeaveGuild simulates a MsgLeaveGuild message using direct keeper calls.
// This bypasses the maintenance mode check for simulation purposes.
// Full maintenance mode testing should be done in integration tests.
func SimulateMsgLeaveGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Find a member who can leave (not a founder)
		membership, memberAddr, err := findGuildMembership(r, ctx, k)
		if err != nil || membership == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "no guild memberships found"), nil, nil
		}

		// Check if the member is the founder of the guild
		if isFounder(ctx, k, membership.GuildId, memberAddr) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "cannot leave as founder"), nil, nil
		}

		// Use direct keeper calls to leave guild (bypasses maintenance mode check)

		// Remove membership
		if err := k.GuildMembership.Remove(ctx, memberAddr); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "failed to remove membership"), nil, nil
		}

		// Update profile to clear guild ID
		profile, err := k.MemberProfile.Get(ctx, memberAddr)
		if err == nil {
			profile.GuildId = 0
			k.MemberProfile.Set(ctx, memberAddr, profile)
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "ok (direct keeper call)"), nil, nil
	}
}

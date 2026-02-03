package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

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

		// Find the simAccount for this member
		simAccount, found := getAccountForAddress(memberAddr, accs)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "member account not found in sim accounts"), nil, nil
		}

		// Ensure member has a profile with correct GuildId
		if err := getOrCreateMemberProfile(r, ctx, k, memberAddr); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "failed to create profile"), nil, nil
		}

		// Verify profile has guild ID set (double check)
		profile, err := k.MemberProfile.Get(ctx, memberAddr)
		if err != nil || profile.GuildId == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLeaveGuild{}), "profile not in guild"), nil, nil
		}

		msg := &types.MsgLeaveGuild{
			Creator: simAccount.Address.String(),
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}

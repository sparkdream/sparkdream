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

func SimulateMsgPromoteToOfficer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a guild where this account is the founder
		guild, guildID, err := findGuildByFounder(r, ctx, k, simAccount.Address.String())
		if err != nil || guild == nil {
			guildID, err = getOrCreateGuild(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "failed to get/create guild"), nil, nil
			}
			// Load the guild we just created/found
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "guild not active"), nil, nil
		}

		// Find a regular member to promote
		memberAccount, _ := simtypes.RandomAcc(r, accs)
		if memberAccount.Address.String() == simAccount.Address.String() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "cannot promote self"), nil, nil
		}

		// Ensure they're a member
		if err := getOrCreateGuildMember(r, ctx, k, guildID, memberAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "failed to add member"), nil, nil
		}

		// Ensure they have a profile with the correct guild ID
		if err := getOrCreateMemberProfile(r, ctx, k, memberAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "failed to create profile"), nil, nil
		}

		// The two remaining handler preconditions. A member promoted by an
		// earlier operation in the same run is still a plausible random pick,
		// and a guild fills up its officer slots as the run goes on. Skip
		// instead of failing delivery: an operation that returns an error
		// aborts the whole simulation run, so an unchecked precondition here
		// is a latent abort that surfaces only on the seeds that reach it.
		if k.IsGuildOfficer(ctx, guildID, memberAccount.Address.String()) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "member is already an officer"), nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "failed to read params"), nil, nil
		}
		current, err := k.Guild.Get(ctx, guildID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "failed to reload guild"), nil, nil
		}
		if uint32(len(current.Officers)) >= params.MaxGuildOfficers {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPromoteToOfficer{}), "guild is at its officer cap"), nil, nil
		}

		msg := &types.MsgPromoteToOfficer{
			Creator: simAccount.Address.String(),
			GuildId: guildID,
			Member:  memberAccount.Address.String(),
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

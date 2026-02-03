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

func SimulateMsgDemoteOfficer(
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
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDemoteOfficer{}), "failed to get/create guild"), nil, nil
			}
			guildVal, _ := k.Guild.Get(ctx, guildID)
			guild = &guildVal
		}

		// Check if guild is active
		if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDemoteOfficer{}), "guild not active"), nil, nil
		}

		// Check if there are any officers to demote
		if len(guild.Officers) == 0 {
			// Try to promote someone first so we can demote them
			memberAccount, _ := simtypes.RandomAcc(r, accs)
			if memberAccount.Address.String() != simAccount.Address.String() {
				// Add as member and officer
				if err := getOrCreateGuildMember(r, ctx, k, guildID, memberAccount.Address.String()); err == nil {
					if err := getOrCreateMemberProfile(r, ctx, k, memberAccount.Address.String()); err == nil {
						guild.Officers = append(guild.Officers, memberAccount.Address.String())
						k.Guild.Set(ctx, guildID, *guild)
					}
				}
			}
		}

		// Re-check if there are officers after potential promotion
		if len(guild.Officers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDemoteOfficer{}), "no officers to demote"), nil, nil
		}

		// Pick a random officer to demote
		officerAddr := guild.Officers[r.Intn(len(guild.Officers))]

		// Ensure officer has a profile with the correct guild ID
		if err := getOrCreateMemberProfile(r, ctx, k, officerAddr); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDemoteOfficer{}), "failed to create officer profile"), nil, nil
		}

		msg := &types.MsgDemoteOfficer{
			Creator: simAccount.Address.String(),
			GuildId: guildID,
			Officer: officerAddr,
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

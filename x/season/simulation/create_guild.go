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

// SimulateMsgCreateGuild simulates a MsgCreateGuild message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgCreateGuild(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Check if user is already in a guild
		_, err := k.GuildMembership.Get(ctx, simAccount.Address.String())
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "user already in a guild"), nil, nil
		}

		// Ensure member has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "failed to create profile"), nil, nil
		}

		// Create guild using direct keeper call (bypasses DREAM token check)
		guildID, err := k.GuildSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "failed to get guild ID"), nil, nil
		}

		guild := types.Guild{
			Id:           guildID,
			Name:         randomGuildName(r),
			Description:  "A simulation generated guild",
			Founder:      simAccount.Address.String(),
			InviteOnly:   r.Intn(2) == 1,
			CreatedBlock: ctx.BlockHeight(),
			Status:       types.GuildStatus_GUILD_STATUS_ACTIVE,
		}

		if err := k.Guild.Set(ctx, guildID, guild); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "failed to create guild"), nil, nil
		}

		// Create founder membership
		membership := types.GuildMembership{
			Member:      simAccount.Address.String(),
			GuildId:     guildID,
			JoinedEpoch: 1,
		}

		if err := k.GuildMembership.Set(ctx, simAccount.Address.String(), membership); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "failed to create membership"), nil, nil
		}

		// Update profile with guild ID
		profile, _ := k.MemberProfile.Get(ctx, simAccount.Address.String())
		profile.GuildId = guildID
		k.MemberProfile.Set(ctx, simAccount.Address.String(), profile)

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateGuild{}), "ok (direct keeper call)"), nil, nil
	}
}

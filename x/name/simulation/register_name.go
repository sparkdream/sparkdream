package simulation

import (
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func SimulateMsgRegisterName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk groupkeeper.Keeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Get Current Params
		params := k.GetParams(ctx)
		var simAccount simtypes.Account
		var found bool

		// 2. ATTEMPT 1: Try to find an existing Council Member
		if params.CouncilGroupId > 0 {
			members, err := gk.GroupMembers(ctx, &group.QueryGroupMembersRequest{
				GroupId: params.CouncilGroupId,
			})

			if err == nil {
				rand.Shuffle(len(members.Members), func(i, j int) {
					members.Members[i], members.Members[j] = members.Members[j], members.Members[i]
				})

				for _, member := range members.Members {
					addr, _ := sdk.AccAddressFromBech32(member.Member.Address)
					simAccount, found = simtypes.FindAccount(accs, addr)
					if found {
						break
					}
				}
			}
		}

		// 3. ATTEMPT 2: Fallback (God Mode)
		if !found {
			simAccount, _ = simtypes.RandomAcc(r, accs)

			// Create a new group with this account as a member
			members := []group.MemberRequest{
				{
					Address:  simAccount.Address.String(),
					Weight:   "1",
					Metadata: "sim member",
				},
			}

			createGroupMsg := &group.MsgCreateGroup{
				Admin:    simAccount.Address.String(),
				Members:  members,
				Metadata: "sim council for registration",
			}

			groupRes, err := gk.CreateGroup(ctx, createGroupMsg)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to create sim group"), nil, nil
			}

			params.CouncilGroupId = groupRes.GroupId
			if err := k.SetParams(ctx, params); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to update params"), nil, err
			}
		}

		// 4. Check Solvency
		if !params.RegistrationFee.IsZero() {
			balance := bk.SpendableCoins(ctx, simAccount.Address)
			if balance.AmountOf(params.RegistrationFee.Denom).LT(params.RegistrationFee.Amount) {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "insufficient funds for reg fee"), nil, nil
			}
		}

		// 4.5. CHECK NAME LIMIT (Updated to use GetOwnedNamesCount)
		// We use a hard limit of 5 to match your chain's enforcement.
		const MaxNames = 5

		count, err := k.GetOwnedNamesCount(ctx, simAccount.Address)
		if err == nil {
			if count >= MaxNames {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "max names reached for account"), nil, nil
			}
		}

		// 5. Generate Valid Name
		minLen := int(params.MinNameLength)
		if minLen <= 0 {
			minLen = 3
		}
		maxLen := int(params.MaxNameLength)
		if maxLen <= minLen {
			maxLen = minLen + 10
		}

		nameLen := minLen + r.Intn(maxLen-minLen+1)
		name := strings.ToLower(simtypes.RandStringOfLength(r, nameLen))

		for _, blocked := range params.BlockedNames {
			if name == blocked {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "generated blocked name"), nil, nil
			}
		}

		// Check collision to be safe
		_, foundName := k.GetName(ctx, name)
		if foundName {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "name already exists"), nil, nil
		}

		// 6. Construct Message
		msg := &types.MsgRegisterName{
			Authority: simAccount.Address.String(),
			Name:      name,
			Data:      simtypes.RandStringOfLength(r, 20),
		}

		// 7. Execute Transaction
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(params.RegistrationFee),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}

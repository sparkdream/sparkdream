package simulation

import (
	"math/rand"
	"strings"
	"time"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/math"
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

// Constants used for setup
const CouncilName = "Commons Council"

func SimulateMsgRegisterName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	ck types.CommonsKeeper,
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
		var councilID uint64

		// 2. ATTEMPT 1: Try to find an existing Council Member by looking up the group name
		council, err := ck.GetExtendedGroup(ctx, CouncilName)
		if err == nil {
			councilID = council.GroupId

			membersQuery := &group.QueryGroupMembersRequest{
				GroupId: councilID,
			}
			membersRes, err := gk.GroupMembers(ctx, membersQuery)

			if err == nil {
				rand.Shuffle(len(membersRes.Members), func(i, j int) {
					membersRes.Members[i], membersRes.Members[j] = membersRes.Members[j], membersRes.Members[i]
				})

				for _, member := range membersRes.Members {
					addr, _ := sdk.AccAddressFromBech32(member.Member.Address)
					simAccount, found = simtypes.FindAccount(accs, addr)
					if found {
						break
					}
				}
			}
		}

		// 3. ATTEMPT 2: Fallback (God Mode) - Create a new mock council if not found
		if !found {
			// Find a random account to be the admin
			simAccount, _ = simtypes.RandomAcc(r, accs)

			// 3a. Create a new Group
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
			councilID = groupRes.GroupId

			// 3b. Create a dummy Policy
			decisionPolicy := group.NewThresholdDecisionPolicy(
				"1",
				time.Hour*24,
				time.Duration(0),
			)

			createPolicyMsg, err := group.NewMsgCreateGroupPolicy(simAccount.Address, groupRes.GroupId, "standard", decisionPolicy)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to create policy msg"), nil, nil
			}

			policyRes, err := gk.CreateGroupPolicy(ctx, createPolicyMsg)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to create sim policy"), nil, nil
			}

			// 3c. Register the Extended Group under the hardcoded name in the Commons Keeper
			mockExtendedGroup := commonstypes.ExtendedGroup{
				GroupId:       groupRes.GroupId,
				PolicyAddress: policyRes.Address,
			}
			if err := ck.SetExtendedGroup(ctx, CouncilName, mockExtendedGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to register extended group"), nil, err
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

		// Define explicit high fees to satisfy the AnteHandler check (5M uspark)
		// Random fees are usually too low for the x/commons spam protection.
		fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

		// Use GenAndDeliverTx (explicit fees) instead of GenAndDeliverTxWithRandFees
		return simulation.GenAndDeliverTx(opMsg, fees)
	}
}

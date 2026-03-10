package simulation

import (
	"math/rand"
	"strings"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
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
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		// 1. Get Current Params
		params := k.GetParams(ctx)
		var simAccount simtypes.Account
		var found bool

		// 2. ATTEMPT 1: Try to find an existing Council Member via native collections
		council, err := ck.GetGroup(ctx, CouncilName)
		if err == nil {
			// Shuffle accounts and find one that's a member
			perm := r.Perm(len(accs))
			for _, idx := range perm {
				acc := accs[idx]
				isMember, mErr := ck.HasMember(ctx, CouncilName, acc.Address.String())
				if mErr == nil && isMember {
					simAccount = acc
					found = true
					break
				}
			}
			_ = council
		}

		// 3. ATTEMPT 2: Fallback (God Mode) - Create a mock council and add simAccount as member
		if !found {
			simAccount, _ = simtypes.RandomAcc(r, accs)

			mockGroup := commonstypes.Group{
				GroupId:       uint64(simtypes.RandIntBetween(r, 1, 1000)),
				PolicyAddress: simAccount.Address.String(),
			}
			if err := ck.SetGroup(ctx, CouncilName, mockGroup); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to register extended group"), nil, err
			}
			// Add simAccount as a council member so HasMember check passes
			if err := ck.AddMember(ctx, CouncilName, commonstypes.Member{Address: simAccount.Address.String(), Weight: "1"}); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "failed to add council member"), nil, err
			}
		}

		// 4. Check Solvency (registration fee + explicit gas fees of 5M uspark)
		explicitFees := math.NewInt(5000000)
		totalRequired := explicitFees
		if !params.RegistrationFee.IsZero() && params.RegistrationFee.Denom == "uspark" {
			totalRequired = totalRequired.Add(params.RegistrationFee.Amount)
		}
		balance := bk.SpendableCoins(ctx, simAccount.Address)
		if balance.AmountOf("uspark").LT(totalRequired) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterName{}), "insufficient funds for reg fee + gas"), nil, nil
		}

		// 4.5. CHECK NAME LIMIT
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
		fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5000000)))

		return simulation.GenAndDeliverTx(opMsg, fees)
	}
}

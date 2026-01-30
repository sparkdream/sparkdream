package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgCreateInterim(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a creator
		creator, creatorAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateInterim{}), "failed to get/create creator"), nil, nil
		}

		// Generate interim details
		interimType := randomInterimType(r)

		// Determine reference based on type
		var referenceID uint64
		var referenceType string

		// For simplicity, use project approval or expert testimony
		if interimType == types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL {
			referenceType = "project"
			referenceID = uint64(r.Intn(100) + 1) // Random project ID
		} else if interimType == types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY {
			referenceType = "challenge"
			referenceID = uint64(r.Intn(100) + 1) // Random challenge ID
		} else {
			// Generic work
			referenceType = "general"
			referenceID = 0
		}

		// Random complexity
		complexities := []types.InterimComplexity{
			types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
		}
		complexity := complexities[r.Intn(len(complexities))]

		// Random deadline (1-7 days from now)
		deadline := ctx.BlockTime().Unix() + int64(r.Intn(7*24*3600)+24*3600)

		msg := &types.MsgCreateInterim{
			Creator:       creator.Address,
			InterimType:   interimType,
			ReferenceId:   referenceID,
			ReferenceType: referenceType,
			Complexity:    complexity,
			Deadline:      deadline,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      creatorAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}

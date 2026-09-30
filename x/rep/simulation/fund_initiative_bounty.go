package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgFundInitiativeBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		noop := func(reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFundInitiativeBounty{}), reason), nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noop("failed to get params")
		}

		creator, _, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return noop("failed to get/create creator")
		}
		initID, err := getOrCreateInitiative(r, ctx, k, creator, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
		if err != nil {
			return noop("failed to get/create initiative")
		}
		initiative, err := k.Initiative.Get(ctx, initID)
		if err != nil || initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
			return noop("initiative not OPEN")
		}

		funder, funderAcc, err := getOrCreateMemberWithDream(r, ctx, k, accs, params.MinInitiativeBountyContribution.MulRaw(2))
		if err != nil {
			return noop("failed to get/create funder with DREAM")
		}
		if k.IsAffiliatedWithProject(ctx, initiative, funder.Address) {
			return noop("funder is affiliated with the initiative")
		}

		// Sim initiatives carry micro-DREAM budgets (see calculateBudgetByTier),
		// far under the 1 DREAM minimum contribution, so no bounty would ever
		// fit. Lift this one to a DREAM-scale budget, inside every tier's
		// ceiling, mirroring the allocation as getOrCreateInitiative does.
		bounty := k.GetInitiativeBounty(ctx, initID)
		minBudget := params.MinInitiativeBountyContribution.MulRaw(20)
		if keeper.DerefInt(initiative.Budget).LT(minBudget) {
			delta := minBudget.Sub(keeper.DerefInt(initiative.Budget))
			if project, perr := k.GetProject(ctx, initiative.ProjectId); perr == nil && !project.Permissionless {
				project.AllocatedBudget = keeper.PtrInt(keeper.DerefInt(project.AllocatedBudget).Add(delta))
				_ = k.Project.Set(ctx, project.Id, project)
			}
			initiative.Budget = keeper.PtrInt(minBudget)
			if err := k.Initiative.Set(ctx, initID, initiative); err != nil {
				return noop("failed to lift initiative budget")
			}
		}

		// Size the contribution inside every cap so the delivered tx cannot fail.
		if uint32(len(bounty.Contributions)) >= params.MaxInitiativeBountyContributions {
			return noop("bounty has the maximum number of contributions")
		}
		amount := params.InitiativeBountyMaxBudgetRatio.MulInt(keeper.DerefInt(initiative.Budget)).TruncateInt().Sub(bounty.Amount)
		epoch, err := k.GetCurrentEpoch(ctx)
		if err != nil {
			return noop("failed to read epoch")
		}
		funded := math.ZeroInt()
		if funder.LastInitiativeBountyEpoch == epoch {
			funded = keeper.DerefInt(funder.InitiativeBountyFundedThisEpoch)
		}
		amount = math.MinInt(amount, params.MaxInitiativeBountyPerFunderEpoch.Sub(funded))
		// Leave ~1% of the unlocked balance for decay applied at delivery.
		unlocked := keeper.DerefInt(funder.DreamBalance).Sub(keeper.DerefInt(funder.StakedDream))
		amount = math.MinInt(amount, unlocked.Sub(unlocked.QuoRaw(100)).SubRaw(1))
		if amount.LT(params.MinInitiativeBountyContribution) {
			return noop("no room for a contribution")
		}

		msg := &types.MsgFundInitiativeBounty{
			Funder:       funder.Address,
			InitiativeId: initID,
			Amount:       amount,
		}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      funderAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}

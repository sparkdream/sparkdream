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

func SimulateMsgCloseInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member who will own the parent project. The signer is
		// resolved to the project creator below, satisfying the handler's
		// authorization gate ("only the project creator or operations committee
		// may close an initiative") regardless of which initiative is selected.
		creator, _, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "failed to get/create creator"), nil, nil
		}

		// Close is no longer restricted to untouched listings: the project side
		// can retire work that is already in someone's hands. Exercise that
		// branch a quarter of the time so the in-flight path — which settles a
		// review round and releases a self-assign bond on the way out — is not
		// left untested. The rest of the time, retire an OPEN listing.
		target := types.InitiativeStatus_INITIATIVE_STATUS_OPEN
		if r.Intn(4) == 0 {
			target = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
		}

		initID, err := getOrCreateInitiative(r, ctx, k, creator, target)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "failed to get/create initiative"), nil, nil
		}

		initiative, err := k.Initiative.Get(ctx, initID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "failed to get initiative"), nil, nil
		}

		// Precheck the keeper's preconditions so the delivered tx cannot fail.
		// Close accepts any live status; CHALLENGED and the terminal statuses
		// are rejected.
		switch initiative.Status {
		case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
			types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
			types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
			types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW:
		default:
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "initiative not in a closeable status"), nil, nil
		}

		// A held self-assign bond is unlocked on the way out, which fails if the
		// assignee's staked balance no longer covers it. Rare, and cheaper to
		// skip than to reconcile.
		if keeper.DerefInt(initiative.SelfAssignBond).IsPositive() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "initiative holds a self-assign bond"), nil, nil
		}

		// A funded review bounty is settled or refunded on close, touching the
		// funders' locked balances. Leave those to the bounty ops.
		if k.GetReviewBounty(ctx, initID).Amount.IsPositive() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "initiative carries a review bounty"), nil, nil
		}

		// Resolve the signer to the project creator (the authorized closer).
		project, err := k.Project.Get(ctx, initiative.ProjectId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "failed to get project"), nil, nil
		}
		signerMember := &types.Member{Address: project.Creator}
		signerAcc, found := getAccountFromMember(signerMember, accs)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "project creator not in sim accounts"), nil, nil
		}

		// ReturnBudget errors if the project's allocated budget is below the
		// amount being returned; skip rather than deliver a failing tx if the
		// found initiative's allocation was never mirrored (non-permissionless).
		if !project.Permissionless {
			allocated := keeper.DerefInt(project.AllocatedBudget)
			if allocated.LT(keeper.DerefInt(initiative.Budget)) {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCloseInitiative{}), "project budget allocation below initiative budget"), nil, nil
			}
		}

		msg := &types.MsgCloseInitiative{
			Creator:      project.Creator,
			InitiativeId: initID,
			Reason:       "Simulation close",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      signerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}

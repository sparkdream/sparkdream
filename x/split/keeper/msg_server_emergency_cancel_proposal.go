package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/group"

	"sparkdream/x/split/types"
)

func (k msgServer) EmergencyCancelProposal(goCtx context.Context, msg *types.MsgEmergencyCancelProposal) (*types.MsgEmergencyCancelProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Get Stored Council Params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// 2. SECURITY CHECK: Verify Group Membership
	// Instead of checking exact address match, we check if the signer belongs to the same Group ID.

	// A. Get Info for the "Known" Council Address (Standard Policy)
	knownPolicy, err := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
		Address: params.CommonsCouncilAddress,
	})
	if err != nil {
		return nil, errorsmod.Wrapf(err, "invalid commons council address in params: %s", params.CommonsCouncilAddress)
	}

	// B. Get Info for the Signer (The Veto Policy)
	signerPolicy, err := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
		Address: msg.Authority,
	})
	if err != nil {
		// If signer isn't a group policy at all, reject immediately
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer %s is not a group policy", msg.Authority)
	}

	// C. Compare Group IDs
	// If the signer belongs to the same Group ID as the configured Council, we allow it.
	if knownPolicy.Info.GroupId != signerPolicy.Info.GroupId {
		return nil, errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"signer %s (Group %d) does not belong to Commons Council (Group %d)",
			msg.Authority, signerPolicy.Info.GroupId, knownPolicy.Info.GroupId,
		)
	}

	// 3. Get the Target Proposal
	prop, err := k.govKeeper.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	// 4. Validation: Only kill active proposals
	if prop.Status != v1.StatusVotingPeriod && prop.Status != v1.StatusDepositPeriod {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal is already finalized (status: %s)", prop.Status)
	}

	// 5. Validation: Check Messages inside the Proposal
	msgs, err := prop.GetMsgs()
	if err != nil {
		return nil, err
	}

	for _, msg := range msgs {
		if sdk.MsgTypeURL(msg) == "/sparkdream.split.v1.MsgUpdateParams" {
			// If the proposal is NOT Expedited, we ALLOW the Veto.
			// This forces validators to use the Expedited path (High Deposit, 67% Threshold)
			// if they want to bypass the Council.
			if !prop.Expedited {
				// We just continue, allowing the function to proceed and kill the proposal.
				break
			}

			// If it IS Expedited, we BLOCK the Veto.
			// This guarantees the Super-Majority can always fire the Council.
			return nil, errorsmod.Wrap(
				sdkerrors.ErrUnauthorized,
				"Council cannot veto EXPEDITED changes to split module params (Constitutional Protection)",
			)
		}
	}

	// --- STEP 3: CALCULATE TALLY ---
	// We must manually count the votes NOW to preserve the history.
	// If we don't do this, the 'FinalTallyResult' remains empty forever.
	_, _, tallyResult, err := k.govKeeper.Tally(ctx, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to tally votes during cancellation")
	}

	// --- STEP 4: UPDATE PROPOSAL STATE ---
	// A. Set the Tally Result we just calculated
	prop.FinalTallyResult = &tallyResult

	// B. Set Status to FAILED
	prop.Status = v1.StatusFailed
	prop.FailedReason = "Emergency Cancel by Commons Council"

	// C. Save the updated proposal to store
	err = k.govKeeper.Proposals.Set(ctx, msg.ProposalId, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to update proposal status")
	}

	// --- STEP 5: KILL PROPOSAL AND CLEANUP QUEUES ---
	// Remove from active processing queues so EndBlocker doesn't touch it again.
	if prop.VotingEndTime != nil {
		err = k.govKeeper.ActiveProposalsQueue.Remove(ctx, collections.Join(*prop.VotingEndTime, prop.Id))
		if err != nil {
			return nil, err
		}
	}

	if prop.Status == v1.StatusVotingPeriod {
		err = k.govKeeper.VotingPeriodProposals.Remove(ctx, prop.Id)
		if err != nil {
			return nil, err
		}
	}

	// --- STEP 6: CHARGE DEPOSITS (Instead of Refund)
	// Sends 100% of proposal deposits to the community pool.
	destination := k.authKeeper.GetModuleAddress(distrtypes.ModuleName).String()
	err = k.govKeeper.ChargeDeposit(ctx, prop.Id, destination, "1")
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to charge deposit")
	}

	// --- STEP 7: Emit Event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"emergency_veto",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", msg.ProposalId)),
			sdk.NewAttribute("executor", msg.Authority),
			sdk.NewAttribute("status", "KILLED"),
		),
	)

	return &types.MsgEmergencyCancelProposalResponse{}, nil
}

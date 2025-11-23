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

	"sparkdream/x/commons/types"
)

func (k msgServer) EmergencyCancelProposal(goCtx context.Context, msg *types.MsgEmergencyCancelProposal) (*types.MsgEmergencyCancelProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Get Stored Council Params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// 2. SECURITY CHECK: Verify Authority

	// Attempt to resolve the Stored Council Address as a Group Policy
	knownPolicy, err1 := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
		Address: params.CommonsCouncilAddress,
	})

	if err1 == nil {
		// Scenario A: The Council is a GROUP (Normal Operation)
		// Resolve the Signer (msg.Authority) as a Group Policy
		signerPolicy, err2 := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: msg.Authority,
		})

		if err2 != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer must be a group policy belonging to the council group")
		}

		// Enforce Group ID Match
		if knownPolicy.Info.GroupId != signerPolicy.Info.GroupId {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer group %d != council group %d", signerPolicy.Info.GroupId, knownPolicy.Info.GroupId)
		}
	} else {
		// Scenario B: The Council is a USER (Interim/Emergency Mode)
		// If the stored address is NOT a policy, we assume it's a simple User Account.
		// In this case, we require an Exact Match.
		if msg.Authority != params.CommonsCouncilAddress {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer %s does not match council address %s", msg.Authority, params.CommonsCouncilAddress)
		}
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
		if sdk.MsgTypeURL(msg) == "/sparkdream.commons.v1.MsgUpdateParams" {
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
				"Council cannot veto EXPEDITED changes to commons module params (Constitutional Protection)",
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

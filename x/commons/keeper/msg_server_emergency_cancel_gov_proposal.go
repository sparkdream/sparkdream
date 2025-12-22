package keeper

import (
	"context"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"sparkdream/x/commons/types"
)

func (k msgServer) EmergencyCancelGovProposal(goCtx context.Context, msg *types.MsgEmergencyCancelGovProposal) (*types.MsgEmergencyCancelGovProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// --- STEP 1: PERMISSION CHECK (RBAC) ---
	// We verify if the signer (msg.Authority) has been explicitly granted
	// the permission to execute this specific message type.

	// 1. Retrieve the permissions object for the signer
	perms, err := k.PolicyPermissions.Get(ctx, msg.Authority)
	if err != nil {
		// If the key doesn't exist, they have 0 permissions.
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer %s has no permissions configured", msg.Authority)
		}
		return nil, err
	}

	// 2. Identify the required permission string (The Message Type URL)
	// We use the SDK to generate the exact string key expected in the list.
	requiredPermission := sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})

	// 3. Check if the permission exists in the allowed list
	if !slices.Contains(perms.AllowedMessages, requiredPermission) {
		return nil, errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"signer %s lacks the required permission: %s",
			msg.Authority, requiredPermission,
		)
	}

	// --- STEP 2: GET TARGET PROPOSAL ---
	prop, err := k.govKeeper.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	// --- STEP 3: VALIDATE PROPOSAL STATUS ---
	// Only active proposals (Deposit or Voting period) can be cancelled.
	if prop.Status != v1.StatusVotingPeriod && prop.Status != v1.StatusDepositPeriod {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal is already finalized (status: %s)", prop.Status)
	}

	// --- STEP 4: CONSTITUTIONAL PROTECTION (The "Super-Majority" Guard) ---
	// Even if a Group has the "Emergency Cancel" permission, they cannot use it
	// to block a Constitutional Amendment (MsgUpdateParams) OR a Membership Overwrite
	// (MsgRenewGroup) IF the community is running it via the EXPEDITED track.
	msgs, err := prop.GetMsgs()
	if err != nil {
		return nil, err
	}

	for _, propMsg := range msgs {
		typeURL := sdk.MsgTypeURL(propMsg)

		if typeURL == "/sparkdream.commons.v1.MsgUpdateParams" ||
			typeURL == "/sparkdream.commons.v1.MsgRenewGroup" {

			// If the proposal is Expedited, it implies a higher quorum/threshold/deposit.
			// This represents a strong community will that overrides Group permissions.
			if prop.Expedited {
				return nil, errorsmod.Wrapf(
					sdkerrors.ErrUnauthorized,
					"PERMISSION DENIED: Groups cannot veto EXPEDITED execution of %s (Constitutional Protection)",
					typeURL,
				)
			}
			// If it is NOT Expedited, we allow the cancellation to proceed.
			break
		}
	}

	// --- STEP 5: CALCULATE TALLY (SNAPSHOT) ---
	// We tally the votes now to preserve the history of the vote distribution
	// at the exact moment it was killed.
	_, _, tallyResult, err := k.govKeeper.Tally(ctx, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to tally votes during cancellation")
	}

	// --- STEP 6: UPDATE PROPOSAL STATE ---
	prop.FinalTallyResult = &tallyResult
	prop.Status = v1.StatusFailed
	prop.FailedReason = fmt.Sprintf("Emergency Cancel executed by Authority: %s", msg.Authority)

	err = k.govKeeper.Proposals.Set(ctx, msg.ProposalId, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to update proposal status")
	}

	// --- STEP 7: CLEANUP QUEUES ---
	// Remove from Active Queue to stop EndBlocker processing
	if prop.VotingEndTime != nil {
		err = k.govKeeper.ActiveProposalsQueue.Remove(ctx, collections.Join(*prop.VotingEndTime, prop.Id))
		if err != nil {
			return nil, err
		}
	}

	// Remove from Voting Period Queue
	if prop.Status == v1.StatusVotingPeriod {
		err = k.govKeeper.VotingPeriodProposals.Remove(ctx, prop.Id)
		if err != nil {
			return nil, err
		}
	}

	// --- STEP 8: CHARGE DEPOSITS ---
	// Deposits are burned (sent to community pool) to prevent spam/abuse.
	// We charge "1" (100% of the deposit).
	destination := k.authKeeper.GetModuleAddress(distrtypes.ModuleName).String()
	err = k.govKeeper.ChargeDeposit(ctx, prop.Id, destination, "1")
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to charge deposit")
	}

	// --- STEP 9: EMIT EVENT ---
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"emergency_veto",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", msg.ProposalId)),
			sdk.NewAttribute("executor", msg.Authority),
			sdk.NewAttribute("status", "KILLED"),
		),
	)

	return &types.MsgEmergencyCancelGovProposalResponse{}, nil
}

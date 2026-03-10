package keeper

import (
	"context"
	"fmt"
	"slices"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"sparkdream/x/commons/types"
)

func (k msgServer) EmergencyCancelGovProposal(goCtx context.Context, msg *types.MsgEmergencyCancelGovProposal) (*types.MsgEmergencyCancelGovProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.late.govKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "gov keeper not configured")
	}

	// STEP 1: PERMISSION CHECK
	perms, err := k.PolicyPermissions.Get(ctx, msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "signer %s has no permissions configured", msg.Authority)
	}

	requiredPermission := sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})
	if !slices.Contains(perms.AllowedMessages, requiredPermission) {
		return nil, errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"signer %s lacks the required permission: %s",
			msg.Authority, requiredPermission,
		)
	}

	// STEP 2: GET TARGET PROPOSAL
	prop, err := k.late.govKeeper.GetProposal(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	// STEP 3: VALIDATE STATUS
	if prop.Status != v1.StatusVotingPeriod && prop.Status != v1.StatusDepositPeriod {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal is already finalized (status: %s)", prop.Status)
	}

	// STEP 4: CONSTITUTIONAL PROTECTION
	msgs, err := prop.GetMsgs()
	if err != nil {
		return nil, err
	}
	for _, propMsg := range msgs {
		typeURL := sdk.MsgTypeURL(propMsg)
		if typeURL == "/sparkdream.commons.v1.MsgUpdateParams" ||
			typeURL == "/sparkdream.commons.v1.MsgRenewGroup" {
			if prop.Expedited {
				return nil, errorsmod.Wrapf(
					sdkerrors.ErrUnauthorized,
					"PERMISSION DENIED: Groups cannot veto EXPEDITED execution of %s (Constitutional Protection)",
					typeURL,
				)
			}
			break
		}
	}

	// STEP 5: TALLY
	_, _, tallyResult, err := k.late.govKeeper.Tally(ctx, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to tally votes during cancellation")
	}

	// STEP 6: UPDATE STATE
	prop.FinalTallyResult = &tallyResult
	prop.Status = v1.StatusFailed
	prop.FailedReason = fmt.Sprintf("Emergency Cancel executed by Authority: %s", msg.Authority)

	err = k.late.govKeeper.SetProposal(ctx, prop)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to update proposal status")
	}

	// STEP 7: CLEANUP QUEUES
	if prop.VotingEndTime != nil {
		err = k.late.govKeeper.ActiveProposalsQueueRemove(ctx, prop.Id, *prop.VotingEndTime)
		if err != nil {
			return nil, err
		}
	}
	if prop.Status == v1.StatusVotingPeriod {
		err = k.late.govKeeper.VotingPeriodProposalsRemove(ctx, prop.Id)
		if err != nil {
			return nil, err
		}
	}

	// STEP 8: CHARGE DEPOSITS
	destination := k.authKeeper.GetModuleAddress(distrtypes.ModuleName).String()
	err = k.late.govKeeper.ChargeDeposit(ctx, prop.Id, destination, "1")
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to charge deposit")
	}

	// STEP 9: EMIT EVENT
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

package keeper

import (
	"context"
	"fmt"
	commontypes "sparkdream/x/common/types"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) ResolveDispute(goCtx context.Context, msg *types.MsgResolveDispute) (*types.MsgResolveDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// Work on a normalized copy: names are stored lowercase and trimmed.
	normalized := *msg
	normalized.Name = normalizeName(msg.Name)
	msg = &normalized

	// 1. Authority: x/gov, or an executed proposal of the Commons Council or
	// its Operations Committee, resolved through x/commons from the real
	// committee record (never a group looked up by a free-form name, which
	// a council could register; NAM-02).
	if !k.isCouncilOrCommitteePolicy(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "sender %s is not authorized to resolve disputes", msg.Authority)
	}

	// 2. Verify dispute exists and is active
	dispute, found := k.GetDispute(ctx, msg.Name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrDisputeNotFound, "no dispute found for name %q", msg.Name)
	}
	if !dispute.Active {
		return nil, errorsmod.Wrapf(types.ErrDisputeNotActive, "dispute for name %q is not active", msg.Name)
	}

	// new_owner can only name the claimant: a resolution transfers the name
	// to the claimant or leaves it where it is.
	if msg.NewOwner != "" && !k.sameMember(ctx, msg.NewOwner, dispute.Claimant) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "new_owner must be empty or the claimant %s", dispute.Claimant)
	}

	// The owner keeps the whole contest window: an uncontested dispute can
	// be upheld only after it closes.
	if msg.TransferApproved && dispute.ContestChallengeId == "" {
		deadline := dispute.FiledAt + commontypes.Saturate[int64](k.GetParams(ctx).DisputeTimeoutBlocks)
		if ctx.BlockHeight() <= deadline {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"the owner's contest window is open until block %d", deadline)
		}
	}

	// 3. Resolve based on whether contested
	if err := k.resolveDisputeInternal(ctx, dispute, msg.TransferApproved); err != nil {
		return nil, err
	}

	return &types.MsgResolveDisputeResponse{}, nil
}

// claimantEligible reports whether the claimant may still receive the name:
// an active x/rep member below the per-address name cap, the same gates as
// MsgRegisterName and MsgTransferName.
func (k Keeper) claimantEligible(ctx context.Context, claimant string) bool {
	active, err := k.IsActiveRepMember(ctx, claimant)
	if err != nil || !active {
		return false
	}
	addr, err := sdk.AccAddressFromBech32(claimant)
	if err != nil {
		return false
	}
	count, err := k.GetOwnedNamesCount(ctx, addr)
	if err != nil {
		return false
	}
	return count < k.GetParams(ctx).MaxNamesPerAddress
}

// resolveDisputeInternal handles the common dispute resolution logic.
// Used by both ResolveDispute (authority) and BeginBlock (timeout auto-resolution).
func (k Keeper) resolveDisputeInternal(ctx context.Context, dispute types.Dispute, transferApproved bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if !dispute.StakeAmount.IsUint64() {
		return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "dispute stake amount too large for uint64: %s", dispute.StakeAmount.String())
	}
	claimantStake := dispute.StakeAmount.Uint64()
	outcome := "dismissed"

	if dispute.ContestChallengeId != "" {
		contestAmount, err := k.getContestAmount(ctx, dispute.ContestChallengeId)
		if err != nil {
			return err
		}
		contestOwner := k.getContestOwner(ctx, dispute.ContestChallengeId)
		if contestOwner == "" {
			return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "contest stake %s has no owner", dispute.ContestChallengeId)
		}
		// CONTESTED path: winner's stake unlocked, loser's burned
		if transferApproved {
			if !k.claimantEligible(ctx, k.canonicalMember(ctx, dispute.Claimant)) {
				return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
					"claimant %s can no longer receive the name (inactive member or at the name cap); dismiss the dispute instead", dispute.Claimant)
			}
			// Claimant wins: transfer name, unlock claimant stake, burn owner stake
			if err := k.transferName(ctx, dispute.Name, k.canonicalMember(ctx, dispute.Claimant)); err != nil {
				return err
			}
			if err := k.dreamOps.SettleStakes(ctx,
				dispute.Claimant, claimantStake,
				contestOwner, contestAmount,
			); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "settle stakes: %s", err)
			}
			outcome = "upheld"
		} else {
			// Owner wins: name stays, unlock owner stake, burn claimant stake
			if err := k.dreamOps.SettleStakes(ctx,
				contestOwner, contestAmount,
				dispute.Claimant, claimantStake,
			); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "settle stakes: %s", err)
			}
			dispute.ContestSucceeded = true
		}
		// Clean up contest stake
		if err := k.ContestStakes.Remove(ctx, dispute.ContestChallengeId); err != nil {
			sdkCtx.Logger().Error("failed to remove contest stake record", "challenge_id", dispute.ContestChallengeId, "error", err)
		}
	} else {
		// UNCONTESTED path
		switch {
		case transferApproved && !k.claimantEligible(ctx, k.canonicalMember(ctx, dispute.Claimant)):
			// The claimant can no longer hold the name: void the dispute
			// and return the stake (nobody misbehaved).
			if err := k.dreamOps.Unlock(ctx, dispute.Claimant, claimantStake); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "unlock claimant stake: %s", err)
			}
			outcome = "voided"
		case transferApproved:
			// Dispute upheld: transfer name, unlock claimant stake
			if err := k.transferName(ctx, dispute.Name, k.canonicalMember(ctx, dispute.Claimant)); err != nil {
				return err
			}
			if err := k.dreamOps.Unlock(ctx, dispute.Claimant, claimantStake); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "unlock claimant stake: %s", err)
			}
			outcome = "upheld"
		default:
			// Dispute dismissed: name stays, burn claimant stake
			if err := k.dreamOps.BurnLocked(ctx, dispute.Claimant, claimantStake); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "burn claimant stake: %s", err)
			}
		}
	}

	// 4. Clean up dispute stake record
	disputeChallengeID := fmt.Sprintf("name_dispute:%s:%d", dispute.Name, dispute.FiledAt)
	if err := k.DisputeStakes.Remove(ctx, disputeChallengeID); err != nil {
		sdkCtx.Logger().Error("failed to remove dispute stake record", "challenge_id", disputeChallengeID, "error", err)
	}

	// 5. Close the dispute
	dispute.Active = false
	if err := k.SetDispute(ctx, dispute); err != nil {
		return err
	}

	// 6. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"name_dispute_resolved",
			sdk.NewAttribute("name", dispute.Name),
			sdk.NewAttribute("claimant", dispute.Claimant),
			sdk.NewAttribute("outcome", outcome),
			sdk.NewAttribute("contested", fmt.Sprintf("%t", dispute.ContestChallengeId != "")),
		),
	)

	return nil
}

// transferName moves a name from its current owner to the new owner. Used by
// dispute resolution (the only path where ownership changes adversarially);
// for cooperative transfers see msg_server_transfer_name.go. Adversarial
// transfer clears any resolver target and acceptance — the prior owner's
// choice of target should not survive a forced ownership change.
func (k Keeper) transferName(ctx context.Context, name string, newOwnerStr string) error {
	record, recordFound := k.GetName(ctx, name)

	// Clear any accepted-target relationship: the prior target's consent was
	// tied to the prior owner's identity; the new owner must re-establish it.
	if recordFound {
		if err := k.clearTargetConsent(ctx, record); err != nil {
			return err
		}
	}

	// Remove from old owner
	currentOwner, found := k.GetNameOwner(ctx, name)
	if found {
		if err := k.RemoveNameFromOwner(ctx, currentOwner, name); err != nil {
			return err
		}

		// Clear old owner's PrimaryName if it matches the transferred name.
		oldOwnerInfo, err := k.Owners.Get(ctx, currentOwner.String())
		if err == nil && oldOwnerInfo.PrimaryName == name {
			oldOwnerInfo.PrimaryName = ""
			if err := k.Owners.Set(ctx, currentOwner.String(), oldOwnerInfo); err != nil {
				return err
			}
		}
	}

	// Add to new owner
	newOwner, err := sdk.AccAddressFromBech32(newOwnerStr)
	if err != nil {
		return err
	}
	if err := k.AddNameToOwner(ctx, newOwner, name); err != nil {
		return err
	}

	// Update record (and drop forward target so the new owner starts clean).
	if !recordFound {
		record = types.NameRecord{Name: name}
	}
	record.Owner = newOwnerStr
	record.Target = ""
	record.TargetAccepted = false
	return k.SetName(ctx, record)
}

// getContestOwner returns the owner address from a ContestStake record.
func (k Keeper) getContestOwner(ctx context.Context, challengeID string) string {
	stake, err := k.ContestStakes.Get(ctx, challengeID)
	if err != nil {
		return ""
	}
	return stake.Owner
}

// getContestAmount returns the stake amount from a ContestStake record. A
// contested dispute without its stake record is inconsistent state (only an
// inconsistent genesis can produce it, see genesis Validate), reported
// explicitly instead of settling a zero stake.
func (k Keeper) getContestAmount(ctx context.Context, challengeID string) (uint64, error) {
	stake, err := k.ContestStakes.Get(ctx, challengeID)
	if err != nil {
		return 0, errorsmod.Wrapf(types.ErrDREAMOperationFailed, "contest stake %s not found", challengeID)
	}
	if !stake.Amount.IsUint64() {
		return 0, errorsmod.Wrapf(types.ErrDREAMOperationFailed, "contest stake amount too large for uint64: %s", stake.Amount.String())
	}
	return stake.Amount.Uint64(), nil
}

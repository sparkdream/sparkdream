package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) ResolveDispute(goCtx context.Context, msg *types.MsgResolveDispute) (*types.MsgResolveDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Authority Check: governance authority, Council policy, or Operations Committee
	authorityAddr, err := k.addressCodec.BytesToString(k.authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to encode authority address")
	}

	authorized := msg.Authority == authorityAddr
	if !authorized {
		// Check if sender is Commons Council policy
		councilGroup, err := k.commonsKeeper.GetGroup(ctx, "Commons Council")
		if err == nil && msg.Authority == councilGroup.PolicyAddress {
			authorized = true
		}
	}
	if !authorized {
		// Check if sender is Operations Committee policy
		opsGroup, err := k.commonsKeeper.GetGroup(ctx, "Operations Committee")
		if err == nil && msg.Authority == opsGroup.PolicyAddress {
			authorized = true
		}
	}
	if !authorized {
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

	// 3. Resolve based on whether contested
	if err := k.resolveDisputeInternal(ctx, dispute, msg.TransferApproved); err != nil {
		return nil, err
	}

	return &types.MsgResolveDisputeResponse{}, nil
}

// resolveDisputeInternal handles the common dispute resolution logic.
// Used by both ResolveDispute (authority) and BeginBlock (timeout auto-resolution).
func (k Keeper) resolveDisputeInternal(ctx context.Context, dispute types.Dispute, transferApproved bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if dispute.ContestChallengeId != "" {
		// CONTESTED path: winner's stake unlocked, loser's burned
		if transferApproved {
			// Claimant wins: transfer name, unlock claimant stake, burn owner stake
			if err := k.transferName(ctx, dispute.Name, dispute.Claimant); err != nil {
				return err
			}
			if err := k.dreamOps.SettleStakes(ctx,
				dispute.Claimant, dispute.StakeAmount.Uint64(),
				k.getContestOwner(ctx, dispute.ContestChallengeId), k.getContestAmount(ctx, dispute.ContestChallengeId),
			); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "settle stakes: %s", err)
			}
		} else {
			// Owner wins: name stays, unlock owner stake, burn claimant stake
			contestOwner := k.getContestOwner(ctx, dispute.ContestChallengeId)
			contestAmount := k.getContestAmount(ctx, dispute.ContestChallengeId)
			if err := k.dreamOps.SettleStakes(ctx,
				contestOwner, contestAmount,
				dispute.Claimant, dispute.StakeAmount.Uint64(),
			); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "settle stakes: %s", err)
			}
		}
		// Clean up contest stake
		k.ContestStakes.Remove(ctx, dispute.ContestChallengeId) //nolint:errcheck
	} else {
		// UNCONTESTED path
		if transferApproved {
			// Dispute upheld: transfer name, unlock claimant stake
			if err := k.transferName(ctx, dispute.Name, dispute.Claimant); err != nil {
				return err
			}
			if err := k.dreamOps.Unlock(ctx, dispute.Claimant, dispute.StakeAmount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "unlock claimant stake: %s", err)
			}
		} else {
			// Dispute dismissed: name stays, burn claimant stake
			if err := k.dreamOps.Burn(ctx, dispute.Claimant, dispute.StakeAmount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "burn claimant stake: %s", err)
			}
		}
	}

	// 4. Clean up dispute stake record
	disputeChallengeID := fmt.Sprintf("name_dispute:%s:%d", dispute.Name, dispute.FiledAt)
	k.DisputeStakes.Remove(ctx, disputeChallengeID) //nolint:errcheck

	// 5. Close the dispute
	dispute.Active = false
	if err := k.SetDispute(ctx, dispute); err != nil {
		return err
	}

	// 6. Emit event
	outcome := "dismissed"
	if transferApproved {
		outcome = "upheld"
	}
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

// transferName moves a name from its current owner to the new owner.
func (k Keeper) transferName(ctx context.Context, name string, newOwnerStr string) error {
	// Remove from old owner
	currentOwner, found := k.GetNameOwner(ctx, name)
	if found {
		k.RemoveNameFromOwner(ctx, currentOwner, name) //nolint:errcheck
	}

	// Add to new owner
	newOwner, err := sdk.AccAddressFromBech32(newOwnerStr)
	if err != nil {
		return err
	}
	if err := k.AddNameToOwner(ctx, newOwner, name); err != nil {
		return err
	}

	// Update record
	record, found := k.GetName(ctx, name)
	if !found {
		record = types.NameRecord{Name: name}
	}
	record.Owner = newOwnerStr
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

// getContestAmount returns the stake amount from a ContestStake record.
func (k Keeper) getContestAmount(ctx context.Context, challengeID string) uint64 {
	stake, err := k.ContestStakes.Get(ctx, challengeID)
	if err != nil {
		return 0
	}
	return stake.Amount.Uint64()
}

package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) SetPrimary(goCtx context.Context, msg *types.MsgSetPrimary) (*types.MsgSetPrimaryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Parse Authority Address
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}

	// 2. Normalize Name
	name := strings.ToLower(strings.TrimSpace(msg.Name))

	// 3. Authorization: signer must either own the name OR be the accepted
	// resolver target. Accepted-target authorization lets an address whose
	// owner has pointed a name at it (and which has consented via
	// MsgAcceptTarget) set that name as its primary, enabling reverse
	// resolution to return a name the signer does not own. The acceptance
	// step is what protects against identity hijacking — see
	// docs/x-name-spec.md §7 for the rationale.
	record, found := k.GetName(ctx, name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name '%s' does not exist", name)
	}

	switch {
	case record.Owner == msg.Authority:
		// owner path — always allowed
	case record.Target == msg.Authority && record.TargetAccepted:
		// accepted-target path — explicit consent already recorded
	case record.Target == msg.Authority && !record.TargetAccepted:
		return nil, errorsmod.Wrapf(types.ErrTargetNotAccepted, "you must call AcceptTarget before setting '%s' as primary", name)
	default:
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "you neither own nor are the accepted target of '%s'", name)
	}

	// 4. Update OwnerInfo
	if err := k.SetPrimaryName(ctx, creatorAddr, name); err != nil {
		return nil, err
	}

	// Refresh owner activity so the owner's other names do not become scavengeable.
	if err := k.RecordOwnerActivity(ctx, msg.Authority); err != nil {
		return nil, err
	}

	return &types.MsgSetPrimaryResponse{}, nil
}

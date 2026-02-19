package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) UpdateCollaboratorRole(ctx context.Context, msg *types.MsgUpdateCollaboratorRole) (*types.MsgUpdateCollaboratorRoleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Address); err != nil {
		return nil, errorsmod.Wrap(err, "invalid collaborator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// Only owner can grant/revoke ADMIN
	if msg.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN && coll.Owner != msg.Creator {
		return nil, types.ErrAdminOnlyOwner
	}

	// If demoting from ADMIN, also only owner
	compositeKey := CollaboratorCompositeKey(coll.Id, msg.Address)
	collab, err := k.Collaborator.Get(ctx, compositeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "address %s is not a collaborator", msg.Address)
	}

	if collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN && coll.Owner != msg.Creator {
		return nil, types.ErrAdminOnlyOwner
	}

	// Must be owner or admin to update roles
	isOwnerAdmin, err := k.IsOwnerOrAdmin(ctx, coll, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check authorization")
	}
	if !isOwnerAdmin {
		return nil, types.ErrUnauthorized
	}

	// Update role
	collab.Role = msg.Role
	if err := k.Collaborator.Set(ctx, compositeKey, collab); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collaborator")
	}

	// Update collection timestamp
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collaborator_role_updated",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("address", msg.Address),
		sdk.NewAttribute("new_role", msg.Role.String()),
		sdk.NewAttribute("updated_by", msg.Creator),
	))

	return &types.MsgUpdateCollaboratorRoleResponse{}, nil
}

package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func (k msgServer) isAuthorized(actor string, policyAddress string) bool {
	// 1. SUPREME AUTHORITY: Allow x/gov module (The "Community")
	// This allows proposals to overwrite any permission.
	govAddress := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	if actor == govAddress {
		return true
	}

	// 2. SELF-REGULATION: Allow the Policy to edit itself
	// This allows the Council to voluntarily drop permissions.
	if actor == policyAddress {
		return true
	}

	return false
}

func (k msgServer) CreatePolicyPermissions(ctx context.Context, msg *types.MsgCreatePolicyPermissions) (*types.MsgCreatePolicyPermissionsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, fmt.Sprintf("invalid address: %s", err))
	}

	if !k.isAuthorized(msg.Authority, msg.PolicyAddress) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only x/gov or the policy itself can define permissions")
	}

	// Check if the value exists
	ok, err := k.PolicyPermissions.Has(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	} else if ok {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "permissions already set for this policy")
	}

	var policyPermissions = types.PolicyPermissions{
		PolicyAddress:   msg.PolicyAddress,
		AllowedMessages: msg.AllowedMessages,
	}

	if err := k.PolicyPermissions.Set(ctx, policyPermissions.PolicyAddress, policyPermissions); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	}

	return &types.MsgCreatePolicyPermissionsResponse{}, nil
}

func (k msgServer) UpdatePolicyPermissions(ctx context.Context, msg *types.MsgUpdatePolicyPermissions) (*types.MsgUpdatePolicyPermissionsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, fmt.Sprintf("invalid signer address: %s", err))
	}

	if !k.isAuthorized(msg.Authority, msg.PolicyAddress) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "unauthorized to update these permissions")
	}

	// Check if the value exists
	val, err := k.PolicyPermissions.Get(ctx, msg.PolicyAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrap(sdkerrors.ErrKeyNotFound, "policy permissions not found")
		}
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	}

	// RATCHET DOWN LOGIC
	// If the signer is the Policy itself (Self-Regulation), it is forbidden from ADDING new permissions.
	// It can only submit a list that is a SUBSET of the existing permissions.
	if msg.Authority == msg.PolicyAddress {
		for _, newMsg := range msg.AllowedMessages {
			// If the new message was NOT in the old list, it's an expansion of power -> REJECT.
			if !slices.Contains(val.AllowedMessages, newMsg) {
				return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
					"ratchet down violation: policy cannot add new permission '%s', only remove existing ones", newMsg)
			}
		}
	}

	var policyPermissions = types.PolicyPermissions{
		PolicyAddress:   msg.PolicyAddress,
		AllowedMessages: msg.AllowedMessages,
	}

	if err := k.PolicyPermissions.Set(ctx, policyPermissions.PolicyAddress, policyPermissions); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "failed to update policyPermissions")
	}

	return &types.MsgUpdatePolicyPermissionsResponse{}, nil
}

func (k msgServer) DeletePolicyPermissions(ctx context.Context, msg *types.MsgDeletePolicyPermissions) (*types.MsgDeletePolicyPermissionsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, fmt.Sprintf("invalid signer address: %s", err))
	}

	if !k.isAuthorized(msg.Authority, msg.PolicyAddress) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "unauthorized to delete these permissions")
	}

	// Check if the value exists
	_, err := k.PolicyPermissions.Get(ctx, msg.PolicyAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrap(sdkerrors.ErrKeyNotFound, "index not set")
		}

		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	}

	if err := k.PolicyPermissions.Remove(ctx, msg.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "failed to remove policyPermissions")
	}

	return &types.MsgDeletePolicyPermissionsResponse{}, nil
}

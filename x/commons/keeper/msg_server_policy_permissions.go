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

// Define messages that are too dangerous for self-administration.
// These can ONLY be granted if the signer (Authority) is the x/gov module.
var RestrictedMessages = map[string]bool{
	"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal": true, // The Veto Gun
	"/sparkdream.commons.v1.MsgUpdateParams":               true, // Changing module rules
	"/sparkdream.commons.v1.MsgForceUpgrade":               true, // Upgrading the chain
}

func (k msgServer) isAuthorized(actor string, policyAddress string) bool {
	// 1. SUPREME AUTHORITY: Allow x/gov module (The "Community")
	govAddress := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	if actor == govAddress {
		return true
	}

	// 2. SELF-REGULATION: Allow the Policy to edit itself
	if actor == policyAddress {
		return true
	}

	return false
}

// ValidatePermissions checks for Forbidden messages AND enforces Restricted messages
func (k msgServer) ValidatePermissions(authority string, msgs []string) error {
	govAddress := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()

	for _, msgType := range msgs {
		// 1. GLOBAL BAN (Forbidden)
		// These messages can never be granted to anyone (e.g., recursive calls)
		if types.ForbiddenMessages[msgType] {
			return errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"SECURITY RISK: Message type '%s' is globally forbidden.",
				msgType)
		}

		// 2. GOVERNANCE EXCLUSIVE (Restricted)
		// If the message is Restricted, the Authority MUST be x/gov.
		if RestrictedMessages[msgType] {
			if authority != govAddress {
				return errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
					"PERMISSION DENIED: Message '%s' is Restricted. It can only be granted by a Governance Proposal (x/gov), not by the group itself.",
					msgType)
			}
		}
	}
	return nil
}

func (k msgServer) CreatePolicyPermissions(ctx context.Context, msg *types.MsgCreatePolicyPermissions) (*types.MsgCreatePolicyPermissionsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, fmt.Sprintf("invalid address: %s", err))
	}

	if !k.isAuthorized(msg.Authority, msg.PolicyAddress) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only x/gov or the policy itself can define permissions")
	}

	// Pass the Authority to the validation function
	if err := k.ValidatePermissions(msg.Authority, msg.AllowedMessages); err != nil {
		return nil, err
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

	// Pass the Authority to the validation function
	if err := k.ValidatePermissions(msg.Authority, msg.AllowedMessages); err != nil {
		return nil, err
	}

	// Check if the value exists
	val, err := k.PolicyPermissions.Get(ctx, msg.PolicyAddress)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrap(sdkerrors.ErrKeyNotFound, "policy permissions not found")
		}
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	}

	// RATCHET DOWN LOGIC (Self-Regulation Check)
	// If the signer is the Policy itself, it cannot ADD new permissions, only remove/keep existing ones.
	// EXCEPTION: If the signer is x/gov, it can add anything (including Restricted msgs).
	govAddress := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()

	if msg.Authority != govAddress {
		for _, newMsg := range msg.AllowedMessages {
			if !slices.Contains(val.AllowedMessages, newMsg) {
				return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
					"ratchet down violation: policy cannot add new permission '%s', only remove existing ones. Submit a Governance Proposal to expand powers.", newMsg)
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

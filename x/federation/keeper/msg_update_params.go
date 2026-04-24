package keeper

import (
	"bytes"
	"context"
	"slices"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

func (k msgServer) UpdateParams(ctx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	// Content type removal check: if known_content_types reduced, auto-strip from PeerPolicies
	currentParams, err := k.Params.Get(ctx)
	if err == nil {
		removedTypes := findRemovedTypes(currentParams.KnownContentTypes, req.Params.KnownContentTypes)
		if len(removedTypes) > 0 {
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			if err := k.stripRemovedContentTypes(ctx, sdkCtx, removedTypes); err != nil {
				return nil, err
			}
		}
	}

	if err := k.Params.Set(ctx, req.Params); err != nil {
		return nil, err
	}

	// Write-through the verifier bond-role config so any change to
	// min_verifier_bond / min_verifier_trust_level / verifier_demotion_cooldown
	// / verifier_recovery_threshold lands on x/rep's BondedRoleConfig.
	if err := k.SyncVerifierBondedRoleConfig(ctx, req.Params); err != nil {
		return nil, errorsmod.Wrap(err, "failed to sync verifier bonded-role config to rep")
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// findRemovedTypes returns content types present in old but not in new.
func findRemovedTypes(old, new []string) []string {
	var removed []string
	for _, ct := range old {
		if !slices.Contains(new, ct) {
			removed = append(removed, ct)
		}
	}
	return removed
}

// stripRemovedContentTypes removes deleted content types from all PeerPolicies.
func (k msgServer) stripRemovedContentTypes(ctx context.Context, sdkCtx sdk.Context, removedTypes []string) error {
	return k.PeerPolicies.Walk(ctx, nil, func(peerID string, policy types.PeerPolicy) (bool, error) {
		modified := false

		newInbound := filterOut(policy.InboundContentTypes, removedTypes)
		if len(newInbound) != len(policy.InboundContentTypes) {
			policy.InboundContentTypes = newInbound
			modified = true
		}

		newOutbound := filterOut(policy.OutboundContentTypes, removedTypes)
		if len(newOutbound) != len(policy.OutboundContentTypes) {
			policy.OutboundContentTypes = newOutbound
			modified = true
		}

		if modified {
			if err := k.PeerPolicies.Set(ctx, peerID, policy); err != nil {
				return true, err
			}
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(types.EventTypePeerPolicyAutoUpdated,
					sdk.NewAttribute(types.AttributeKeyPeerID, peerID)),
			)
		}
		return false, nil
	})
}

// filterOut returns a new slice with all entries from removedTypes removed.
func filterOut(items []string, removedTypes []string) []string {
	var result []string
	for _, item := range items {
		if !slices.Contains(removedTypes, item) {
			result = append(result, item)
		}
	}
	return result
}

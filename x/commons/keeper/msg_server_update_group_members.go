package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) UpdateGroupMembers(goCtx context.Context, msg *types.MsgUpdateGroupMembers) (*types.MsgUpdateGroupMembersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Identify the target group via policy address
	_, targetGroup, found := k.getGroupByPolicy(ctx, msg.GroupPolicyAddress)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "target group %s not found", msg.GroupPolicyAddress)
	}

	// Get council name for Members collection
	councilName, err := k.PolicyToName.Get(ctx, msg.GroupPolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "council name not found for %s", msg.GroupPolicyAddress)
	}

	// 2. Authorization
	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()
	signerAddr := msg.Authority

	isGov := (signerAddr == govAddr)
	isParent := (targetGroup.ParentPolicyAddress == signerAddr)
	isElectoral := false
	if targetGroup.ElectoralPolicyAddress != "" && targetGroup.ElectoralPolicyAddress == signerAddr {
		isElectoral = true
	}

	if !isGov && !isParent && !isElectoral {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
			"unauthorized: signer must be x/gov, the parent, or the designated electoral_policy_address")
	}

	// Rate limit check for electoral authority
	var signerGroup types.Group
	var signerName string

	if isElectoral {
		var foundSigner bool
		signerName, signerGroup, foundSigner = k.getGroupByPolicy(ctx, signerAddr)
		if !foundSigner {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "electoral signer not found in registry")
		}

		if ctx.BlockTime().Unix() < signerGroup.LastParentUpdate+signerGroup.UpdateCooldown {
			return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"electoral group cooldown active: wait until %d",
				signerGroup.LastParentUpdate+signerGroup.UpdateCooldown)
		}
	}

	// 3. Prepare inputs
	adds, err := k.parseMembers(msg.MembersToAdd, msg.WeightsToAdd)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid members_to_add: %s", err.Error())
	}

	// 4. Calculate projected count using native Members collection
	currentMembers, err := k.GetCouncilMembers(ctx, councilName)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to fetch current target members")
	}

	memberMap := make(map[string]bool)
	for _, m := range currentMembers {
		memberMap[m.Address] = true
	}
	for _, addrToRemove := range msg.MembersToRemove {
		delete(memberMap, addrToRemove)
	}
	for _, addReq := range adds {
		memberMap[addReq.Address] = true
	}

	projectedCount := uint64(len(memberMap))
	if projectedCount < targetGroup.MinMembers {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"update would reduce group members to %d (min required: %d)",
			projectedCount, targetGroup.MinMembers)
	}
	if projectedCount > targetGroup.MaxMembers {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"update would increase group members to %d (max allowed: %d)",
			projectedCount, targetGroup.MaxMembers)
	}

	// 5. Execute — apply changes to native Members collection
	for _, m := range adds {
		m.AddedAt = ctx.BlockTime().Unix()
		if err := k.Members.Set(ctx, collections.Join(councilName, m.Address), m); err != nil {
			return nil, err
		}
	}
	for _, addr := range msg.MembersToRemove {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid removal address: %s", addr)
		}
		_ = k.Members.Remove(ctx, collections.Join(councilName, addr))
	}

	// 6. Update cooldown
	if isElectoral {
		signerGroup.LastParentUpdate = ctx.BlockTime().Unix()
		if err := k.Groups.Set(ctx, signerName, signerGroup); err != nil {
			return nil, err
		}
	}

	return &types.MsgUpdateGroupMembersResponse{}, nil
}

// getGroupByPolicy finds a group by its Policy Address using O(1) PolicyToName index
func (k msgServer) getGroupByPolicy(ctx context.Context, policyAddr string) (string, types.Group, bool) {
	name, err := k.PolicyToName.Get(ctx, policyAddr)
	if err != nil {
		return "", types.Group{}, false
	}

	group, err := k.Groups.Get(ctx, name)
	if err != nil {
		return "", types.Group{}, false
	}

	return name, group, true
}

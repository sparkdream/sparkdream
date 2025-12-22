package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

func (k msgServer) UpdateGroupMembers(goCtx context.Context, msg *types.MsgUpdateGroupMembers) (*types.MsgUpdateGroupMembersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// ==========================================
	// 1. VERIFY RELATIONSHIP & AUTHORITY
	// ==========================================

	// A. Identify the Target Group (The one being updated)
	// This uses the O(1) Index lookup
	_, targetGroup, found := k.getExtendedGroupByPolicy(ctx, msg.GroupPolicyAddress)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "target group %s not found", msg.GroupPolicyAddress)
	}

	// B. Identify the Signer (The Authority)
	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	signerAddr := msg.Authority

	// C. Check Authorization
	isGov := (signerAddr == govAddr)
	isParent := (targetGroup.ParentPolicyAddress == signerAddr)

	// A designated Electoral Authority if specified can manage the members and renewal of the target group.
	isElectoral := false
	if targetGroup.ElectoralPolicyAddress != "" && targetGroup.ElectoralPolicyAddress == signerAddr {
		isElectoral = true
	}

	if !isGov && !isParent && !isElectoral {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized,
			"unauthorized: signer must be x/gov, the parent, or the designated electoral_policy_address")
	}

	// 2. RATE LIMIT CHECK / COOLDOWN CHECK
	// We apply the cooldown if the update comes from the Delegated Electoral Authority
	var signerGroup types.ExtendedGroup
	var signerName string

	if isElectoral {
		// Fetch signer details to check cooldown
		var foundSigner bool
		signerName, signerGroup, foundSigner = k.getExtendedGroupByPolicy(ctx, signerAddr)
		if !foundSigner {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "electoral signer not found in registry")
		}

		if ctx.BlockTime().Unix() < signerGroup.LastParentUpdate+signerGroup.UpdateCooldown {
			return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"electoral group cooldown active: wait until %d",
				signerGroup.LastParentUpdate+signerGroup.UpdateCooldown)
		}
	}

	// ==========================================
	// 3. PREPARE & VALIDATE INPUTS
	// ==========================================
	adds, err := k.parseMembers(msg.MembersToAdd, msg.WeightsToAdd)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid members_to_add: %s", err.Error())
	}

	// ==========================================
	// 4. CALCULATE PROJECTED MEMBER COUNT
	// ==========================================

	// A. Fetch Current Members (Handle Pagination)
	var currentMembers []*group.GroupMember
	pageReq := &query.PageRequest{
		Limit: 100,
	}

	for {
		resp, err := k.groupKeeper.GroupMembers(ctx, &group.QueryGroupMembersRequest{
			GroupId:    targetGroup.GroupId,
			Pagination: pageReq,
		})
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to fetch current target members")
		}

		currentMembers = append(currentMembers, resp.Members...)

		if resp.Pagination == nil || resp.Pagination.NextKey == nil {
			break
		}
		pageReq.Key = resp.Pagination.NextKey
	}

	// B. Build a Map of the Current State
	memberMap := make(map[string]bool)
	for _, m := range currentMembers {
		memberMap[m.Member.Address] = true
	}

	// C. Simulate Removals
	for _, addrToRemove := range msg.MembersToRemove {
		delete(memberMap, addrToRemove)
	}

	// D. Simulate Additions
	for _, addReq := range adds {
		memberMap[addReq.Address] = true
	}

	// E. Check Bounds
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

	// ==========================================
	// 5. EXECUTE UPDATE
	// ==========================================
	var updates []group.MemberRequest

	// Add the validated additions
	updates = append(updates, adds...)

	// Add the removals (Weight = 0)
	for _, addr := range msg.MembersToRemove {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid removal address: %s", addr)
		}
		updates = append(updates, group.MemberRequest{
			Address:  addr,
			Weight:   "0",
			Metadata: "Removed via Authority Update",
		})
	}

	// Sign as the Module Account (Admin of the Group)
	_, err = k.groupKeeper.UpdateGroupMembers(ctx, &group.MsgUpdateGroupMembers{
		Admin:         k.GetModuleAddress().String(),
		GroupId:       targetGroup.GroupId,
		MemberUpdates: updates,
	})
	if err != nil {
		return nil, err
	}

	// ==========================================
	// 6. UPDATE STATE (COOLDOWN)
	// ==========================================

	// Only update the SIGNER if it was an Electoral -> Parent update.
	if isElectoral {
		signerGroup.LastParentUpdate = ctx.BlockTime().Unix()

		// We save the SIGNER group, because they are the one being rate-limited.
		if err := k.ExtendedGroup.Set(ctx, signerName, signerGroup); err != nil {
			return nil, err
		}
	}

	return &types.MsgUpdateGroupMembersResponse{}, nil
}

// Helper to find a group by its Policy Address
// Uses O(1) PolicyToName index
func (k msgServer) getExtendedGroupByPolicy(ctx context.Context, policyAddr string) (string, types.ExtendedGroup, bool) {
	name, err := k.PolicyToName.Get(ctx, policyAddr)
	if err != nil {
		return "", types.ExtendedGroup{}, false
	}

	group, err := k.ExtendedGroup.Get(ctx, name)
	if err != nil {
		return "", types.ExtendedGroup{}, false
	}

	return name, group, true
}

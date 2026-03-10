package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) DeleteGroup(goCtx context.Context, msg *types.MsgDeleteGroup) (*types.MsgDeleteGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	groupInfo, err := k.Groups.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// Authorization — parent, sibling veto, or x/gov
	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()
	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == groupInfo.ParentPolicyAddress)
	isSiblingPolicy := false

	if !isGov && !isParent {
		// Check if the signer is a sibling veto policy of the parent
		isSiblingPolicy = k.IsSiblingPolicy(ctx, groupInfo.ParentPolicyAddress, msg.Authority)
	}

	if !isGov && !isParent && !isSiblingPolicy {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"unauthorized: signer %s is not the parent, a sibling veto policy, or x/gov", msg.Authority)
	}

	// Clean up x/commons state
	if err := k.Groups.Remove(ctx, msg.GroupName); err != nil {
		return nil, err
	}
	if err := k.PolicyToName.Remove(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove policy index")
	}
	_ = k.PolicyPermissions.Remove(ctx, groupInfo.PolicyAddress)

	if groupInfo.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, groupInfo.PolicyAddress, 0)
	}

	// Invalidate pending proposals via version bump
	if _, err := k.BumpPolicyVersion(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to invalidate pending proposals via version bump")
	}

	// Remove all members
	if err := k.ClearCouncilMembers(ctx, msg.GroupName); err != nil {
		return nil, errorsmod.Wrap(err, "failed to clear members")
	}

	// Clean up veto policy if it exists
	vetoAddr, err := k.VetoPolicies.Get(ctx, msg.GroupName)
	if err == nil {
		_ = k.PolicyToName.Remove(ctx, vetoAddr)
		_ = k.PolicyPermissions.Remove(ctx, vetoAddr)
		_ = k.DecisionPolicies.Remove(ctx, vetoAddr)
		_ = k.VetoPolicies.Remove(ctx, msg.GroupName)
	}

	// Clean up decision policy
	_ = k.DecisionPolicies.Remove(ctx, groupInfo.PolicyAddress)

	return &types.MsgDeleteGroupResponse{}, nil
}

package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) VetoGroupProposals(goCtx context.Context, msg *types.MsgVetoGroupProposals) (*types.MsgVetoGroupProposalsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	groupInfo, err := k.Groups.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// Authorization — parent, sibling veto policy, or x/gov
	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()
	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == groupInfo.ParentPolicyAddress)
	isSiblingPolicy := false

	if !isGov && !isParent {
		isSiblingPolicy = k.IsSiblingPolicy(ctx, groupInfo.ParentPolicyAddress, msg.Authority)
	}

	if !isGov && !isParent && !isSiblingPolicy {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"unauthorized: signer %s is not the parent or a veto policy of the parent council", msg.Authority)
	}

	// Bump policy version — all proposals with old version will fail on execute
	_, err = k.BumpPolicyVersion(ctx, groupInfo.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to bump policy version for veto")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"group_proposals_vetoed",
			sdk.NewAttribute("group_name", msg.GroupName),
			sdk.NewAttribute("executor", msg.Authority),
			sdk.NewAttribute("child_policy", groupInfo.PolicyAddress),
		),
	)

	return &types.MsgVetoGroupProposalsResponse{}, nil
}

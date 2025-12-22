package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

func (k msgServer) VetoGroupProposals(goCtx context.Context, msg *types.MsgVetoGroupProposals) (*types.MsgVetoGroupProposalsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Retrieve the Target Group Info
	groupInfo, err := k.ExtendedGroup.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// 2. AUTHORIZATION CHECK (Parent + Sibling Logic)
	// We must allow the "Recorded Parent" (Standard Policy) AND its "Sibling" (Veto Policy).
	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()

	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == groupInfo.ParentPolicyAddress)
	isSiblingPolicy := false

	if !isGov && !isParent {
		// A. Get Group ID of Recorded Parent (Standard Policy)
		parentPolicyInfo, errParent := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: groupInfo.ParentPolicyAddress,
		})

		// B. Get Group ID of Signer (Veto Policy)
		signerPolicyInfo, errSigner := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: msg.Authority,
		})

		// C. If both exist and share the same Group ID, they are "Siblings" (Same Council)
		if errParent == nil && errSigner == nil {
			if parentPolicyInfo.Info.GroupId == signerPolicyInfo.Info.GroupId {
				isSiblingPolicy = true
			}
		}
	}

	if !isGov && !isParent && !isSiblingPolicy {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"unauthorized: signer %s is not the parent or a veto policy of the parent council", msg.Authority)
	}

	// 3. EXECUTE VETO (Policy Version Bump)
	// By updating the metadata, we increment the Policy Version (v1 -> v2).
	// All proposals created under v1 will fail execution with "ErrWrongPolicyVersion".

	// Fetch current info to append the veto note
	policyInfo, err := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
		Address: groupInfo.PolicyAddress,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "child policy not found")
	}

	newMetadata := policyInfo.Info.Metadata + " [VETOED]"
	if len(newMetadata) > 255 {
		newMetadata = "Vetoed by Parent Council"
	}

	_, err = k.groupKeeper.UpdateGroupPolicyMetadata(ctx, &group.MsgUpdateGroupPolicyMetadata{
		Admin:              k.GetModuleAddress().String(), // x/commons module is the Admin
		GroupPolicyAddress: groupInfo.PolicyAddress,
		Metadata:           newMetadata,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to bump policy version for veto")
	}

	// 4. EMIT EVENT
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

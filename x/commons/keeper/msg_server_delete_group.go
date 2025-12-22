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

func (k msgServer) DeleteGroup(goCtx context.Context, msg *types.MsgDeleteGroup) (*types.MsgDeleteGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Retrieve the Group being deleted
	groupInfo, err := k.ExtendedGroup.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// 2. AUTHORIZATION CHECK
	// Only the Parent, a Sibling Veto Policy of the Parent, or x/gov can delete a group.
	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()

	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == groupInfo.ParentPolicyAddress)
	isSiblingPolicy := false

	// Check 3: Is it a "Sibling Policy" of the Parent?
	// This allows the Veto Policy (which has a different address but same Group ID as Parent)
	// to execute the deletion.
	if !isGov && !isParent {
		// A. Get Group ID of Recorded Parent
		parentPolicyInfo, errParent := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: groupInfo.ParentPolicyAddress,
		})

		// B. Get Group ID of Signer
		signerPolicyInfo, errSigner := k.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: msg.Authority,
		})

		// C. If both exist and share the same Group ID, they are siblings
		if errParent == nil && errSigner == nil {
			if parentPolicyInfo.Info.GroupId == signerPolicyInfo.Info.GroupId {
				isSiblingPolicy = true
			}
		}
	}

	if !isGov && !isParent && !isSiblingPolicy {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"unauthorized: signer %s is not the parent, a sibling veto policy, or x/gov", msg.Authority)
	}

	// 3. CLEAN UP X/COMMONS STATE

	// A. Remove from Registry (ExtendedGroup)
	if err := k.ExtendedGroup.Remove(ctx, msg.GroupName); err != nil {
		return nil, err
	}

	// B. Remove Index (Policy -> Name)
	// CRITICAL: We must remove this to prevent the Cycle Detection from finding "Ghost" groups.
	if err := k.PolicyToName.Remove(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove policy index")
	}

	// C. Remove Permissions (PolicyPermissions)
	_ = k.PolicyPermissions.Remove(ctx, groupInfo.PolicyAddress)

	// D. Stop Funding (Set weight to 0)
	if groupInfo.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, groupInfo.PolicyAddress, 0)
	}

	// 4. KILL ZOMBIE PROPOSALS (The "Poison Pill")
	// Simply removing members isn't enough; proposals in Timelock would still execute.
	// We MUST increment the Policy Version. We do this by updating the metadata to "DELETED".
	// Result: Current Policy Version becomes N+1. All pending proposals (Version N) fail.
	_, err = k.groupKeeper.UpdateGroupPolicyMetadata(ctx, &group.MsgUpdateGroupPolicyMetadata{
		Admin:              k.GetModuleAddress().String(),
		GroupPolicyAddress: groupInfo.PolicyAddress,
		Metadata:           "DELETED",
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to invalidate pending proposals via version bump")
	}

	// 5. CLEAN UP X/GROUP STATE (Best Effort)
	// We remove all members to make the group ID inert for any future attempts.
	membersRes, err := k.groupKeeper.GroupMembers(ctx, &group.QueryGroupMembersRequest{
		GroupId: groupInfo.GroupId,
	})
	if err == nil {
		var updates []group.MemberRequest
		for _, m := range membersRes.Members {
			updates = append(updates, group.MemberRequest{
				Address:  m.Member.Address,
				Weight:   "0", // Setting weight to 0 removes the member
				Metadata: "Group Deleted",
			})
		}

		if len(updates) > 0 {
			_, _ = k.groupKeeper.UpdateGroupMembers(ctx, &group.MsgUpdateGroupMembers{
				Admin:         k.GetModuleAddress().String(),
				GroupId:       groupInfo.GroupId,
				MemberUpdates: updates,
			})
		}
	}

	return &types.MsgDeleteGroupResponse{}, nil
}

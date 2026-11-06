package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

	// Refuse while another group depends on this one (its parent or its
	// electoral authority): delete or re-parent the dependents first. The
	// group set is small (councils, committees and their sub-groups).
	var dependent string
	if err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
		if name != msg.GroupName && (g.ParentPolicyAddress == groupInfo.PolicyAddress || g.ElectoralPolicyAddress == groupInfo.PolicyAddress) {
			dependent = name
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	if dependent != "" {
		return nil, errorsmod.Wrapf(types.ErrGroupHasDependents, "%s depends on %s", dependent, msg.GroupName)
	}

	// Clean up x/commons state
	if err := k.Groups.Remove(ctx, msg.GroupName); err != nil {
		return nil, err
	}
	if err := k.PolicyToName.Remove(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove policy index")
	}
	if err := k.PolicyPermissions.Remove(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, err
	}

	// Stop x/split distributions to the deleted policy: nothing can spend
	// from that address once the group is gone.
	if err := k.splitKeeper.RemoveShareByAddress(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove split share")
	}

	// Revoke every x/session grant the group's policies issued (recurring
	// spends, allowances, oneshots, session keys). Once the group is gone
	// nobody can sign for the policy to cancel them, and the claim hook no
	// longer recognises the granter as a council, so without this they
	// would keep paying out ungated.
	vetoAddr, vetoErr := k.VetoPolicies.Get(ctx, msg.GroupName)
	formerPolicies := []string{groupInfo.PolicyAddress}
	if vetoErr == nil && vetoAddr != "" {
		formerPolicies = append(formerPolicies, vetoAddr)
	}
	for _, policy := range formerPolicies {
		// Tombstone: the claim hook refuses any grant whose granter is a
		// former group policy (defence in depth for grants restored from
		// genesis or created by a path that bypasses the revocation below).
		// Written first: x/session lets commons revoke a grant it did not
		// create only for a live or tombstoned policy (ManagesGranter).
		if err := k.DeletedPolicies.Set(ctx, policy); err != nil {
			return nil, err
		}
		if err := k.revokeGrantsOfPolicy(ctx, policy); err != nil {
			return nil, err
		}
	}

	// Sweep the residual treasury (including refunded oneshot deposits) to
	// the parent policy, which can still spend it under its own gates. With
	// a parent that is not a group policy (x/gov) the funds stay put; gov
	// can recover them with a chain upgrade if ever needed.
	if k.IsGroupPolicyAddress(ctx, groupInfo.ParentPolicyAddress) {
		if err := k.sweepPolicyBalance(ctx, formerPolicies, groupInfo.ParentPolicyAddress); err != nil {
			return nil, err
		}
	}

	// Invalidate pending proposals via version bump.
	// NOTE: This bumps the policy version so that pending proposals fail validation at
	// execution time, but the proposal records themselves remain in storage. A full cleanup
	// would walk all proposals for this group's policy address and set status=EXPIRED, but
	// the version bump achieves the same functional effect (proposals become unexecutable)
	// without the gas cost of a full scan. Orphaned proposal records are harmless storage
	// overhead that can be cleaned up by a future migration or governance action.
	if _, err := k.BumpPolicyVersion(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to invalidate pending proposals via version bump")
	}

	// Cancel the group's open polls (at most MaxOpenPollsPerGroup). Polls
	// are keyed by group name: kept, a group later registered under the
	// same name would inherit them, its members could vote on questions
	// put to the old membership and the old polls would count against its
	// open-poll cap (CMN-105).
	if err := k.cancelOpenPollsOf(ctx, msg.GroupName); err != nil {
		return nil, errorsmod.Wrap(err, "failed to cancel open polls")
	}

	// Remove all members
	if err := k.ClearCouncilMembers(ctx, msg.GroupName); err != nil {
		return nil, errorsmod.Wrap(err, "failed to clear members")
	}

	// Clean up veto policy if it exists
	if vetoErr == nil {
		if err := k.PolicyToName.Remove(ctx, vetoAddr); err != nil {
			return nil, err
		}
		if err := k.PolicyPermissions.Remove(ctx, vetoAddr); err != nil {
			return nil, err
		}
		if err := k.DecisionPolicies.Remove(ctx, vetoAddr); err != nil {
			return nil, err
		}
		if err := k.VetoPolicies.Remove(ctx, msg.GroupName); err != nil {
			return nil, err
		}
	}

	// Clean up decision policy
	if err := k.DecisionPolicies.Remove(ctx, groupInfo.PolicyAddress); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"group_deleted",
		sdk.NewAttribute("group_name", msg.GroupName),
		sdk.NewAttribute("policy_address", groupInfo.PolicyAddress),
		sdk.NewAttribute("authority", msg.Authority),
	))

	return &types.MsgDeleteGroupResponse{}, nil
}

// revokeGrantsOfPolicy revokes every non-terminal x/session grant whose
// granter is policy, through the module-bypass entrypoint x/commons is
// authorized for. Grants already terminal or gone are skipped.
func (k Keeper) revokeGrantsOfPolicy(ctx context.Context, policy string) error {
	sk := k.late.sessionKeeper
	if sk == nil {
		// Unit tests without x/session; app.go always wires it.
		return nil
	}
	grants, err := sk.ListGrantsByGranter(ctx, policy, sessiontypes.GrantType_GRANT_TYPE_UNSPECIFIED)
	if err != nil {
		return errorsmod.Wrap(err, "failed to list the group's session grants")
	}
	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, g := range grants {
		if _, err := sk.RevokeGrantInternal(ctx, commonsModuleAddr, g.Id); err != nil {
			if errors.Is(err, sessiontypes.ErrGrantTerminal) || errors.Is(err, sessiontypes.ErrGrantNotFound) {
				continue
			}
			return errorsmod.Wrapf(err, "failed to revoke session grant %d of deleted group policy", g.Id)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"group_grant_revoked",
			sdk.NewAttribute("grant_id", fmt.Sprintf("%d", g.Id)),
			sdk.NewAttribute("granter", policy),
			sdk.NewAttribute("grantee", g.Grantee),
		))
	}
	return nil
}

// sweepPolicyBalance moves the whole balance of each former policy address
// to recipient.
func (k Keeper) sweepPolicyBalance(ctx context.Context, policies []string, recipient string) error {
	to, err := k.addressCodec.StringToBytes(recipient)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid sweep recipient")
	}
	for _, policy := range policies {
		from, err := k.addressCodec.StringToBytes(policy)
		if err != nil {
			continue
		}
		bal := k.bankKeeper.GetAllBalances(ctx, from)
		if bal.IsZero() {
			continue
		}
		if err := k.bankKeeper.SendCoins(ctx, from, to, bal); err != nil {
			return errorsmod.Wrap(err, "failed to sweep the deleted group's treasury")
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"group_treasury_swept",
			sdk.NewAttribute("from", policy),
			sdk.NewAttribute("to", recipient),
			sdk.NewAttribute("amount", bal.String()),
		))
	}
	return nil
}

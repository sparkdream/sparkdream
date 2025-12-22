package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/errors"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) ForceUpgrade(goCtx context.Context, msg *types.MsgForceUpgrade) (*types.MsgForceUpgradeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. RBAC CHECK: Is the signer authorized?
	// We cannot rely solely on ValidatePermissions because that checks if a permission is valid *to grant*,
	// not if the signer currently *possesses* it.
	//
	// To secure the Proxy, we must strictly verify that the signer (msg.Authority) has the
	// specific "MsgForceUpgrade" Type URL explicitly listed in their PolicyPermissions store.

	perms, err := k.PolicyPermissions.Get(ctx, msg.Authority)
	if err != nil {
		// If no permissions exist for this address, they are unauthorized.
		return nil, errors.Wrapf(sdkerrors.ErrUnauthorized, "signer has no permissions profile found")
	}

	hasPerm := false
	targetMsg := sdk.MsgTypeURL(msg)

	// Iterate through the granted permissions to find an exact match for this message type.
	for _, p := range perms.AllowedMessages {
		if p == targetMsg {
			hasPerm = true
			break
		}
	}

	if !hasPerm {
		return nil, errors.Wrapf(sdkerrors.ErrUnauthorized, "signer %s lacks the specific MsgForceUpgrade permission required to trigger this proxy", msg.Authority)
	}

	// 2. CONVERT SHADOW TYPE TO REAL TYPE
	// We map values from our local 'types.UpgradePlan' to the SDK's 'upgradetypes.Plan'
	realPlan := upgradetypes.Plan{
		Name:   msg.Plan.Name,
		Height: msg.Plan.Height,
		Info:   msg.Plan.Info,
		// Time and Upstream are usually zero/nil for block-height based upgrades
	}

	// 2. EXECUTE UPGRADE
	// Since x/commons is a trusted module (and we just verified the caller has the specific RBAC permission),
	// we call the Upgrade Keeper's ScheduleUpgrade method directly.
	//
	// This approach bypasses the strict "authority == x/gov" check typically found inside
	// x/upgrade's own MsgServer, effectively allowing our Technical Council to act as a second authority.
	err = k.upgradeKeeper.ScheduleUpgrade(ctx, realPlan)
	if err != nil {
		return nil, err
	}

	return &types.MsgForceUpgradeResponse{}, nil
}

package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) UpdateGroupConfig(goCtx context.Context, msg *types.MsgUpdateGroupConfig) (*types.MsgUpdateGroupConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	group, err := k.Groups.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()
	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == group.ParentPolicyAddress)

	if !isGov && !isParent {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"only the parent (%s) or x/gov can update this config", group.ParentPolicyAddress)
	}

	// Validate & update fields
	if msg.MaxSpendPerEpoch != nil {
		if msg.MaxSpendPerEpoch.IsNegative() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch cannot be negative")
		}
		coin := sdk.NewCoin("uspark", *msg.MaxSpendPerEpoch)
		if !coin.IsValid() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch must be valid")
		}
		group.MaxSpendPerEpoch = msg.MaxSpendPerEpoch
	}

	if msg.UpdateCooldown > 0 {
		group.UpdateCooldown = int64(msg.UpdateCooldown)
	}

	if msg.FutarchyEnabled != nil {
		group.FutarchyEnabled = msg.FutarchyEnabled.Value
	}

	targetMin := group.MinMembers
	if msg.MinMembers > 0 {
		targetMin = uint64(msg.MinMembers)
	}
	targetMax := group.MaxMembers
	if msg.MaxMembers > 0 {
		targetMax = uint64(msg.MaxMembers)
	}
	if targetMin == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidGroupSize, "min_members must be > 0")
	}
	if targetMax < targetMin {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"invalid bounds: max_members (%d) cannot be less than min_members (%d)", targetMax, targetMin)
	}
	group.MinMembers = targetMin
	group.MaxMembers = targetMax

	if msg.TermDuration > 0 {
		group.TermDuration = int64(msg.TermDuration)
	}

	if msg.ElectoralPolicyAddress != "" {
		group.ElectoralPolicyAddress = msg.ElectoralPolicyAddress
	}

	// Update decision policy if threshold or policy_type is provided
	if msg.VoteThreshold != nil || msg.PolicyType != "" {
		if msg.PolicyType != "" && msg.VoteThreshold == nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "vote_threshold required when updating decision policy")
		}
		if msg.PolicyType != PolicyTypePercentage && msg.PolicyType != PolicyTypeThreshold {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid policy_type '%s'. Must be '%s' or '%s'", msg.PolicyType, PolicyTypePercentage, PolicyTypeThreshold)
		}
		if msg.VotingPeriod <= 0 {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "voting_period must be greater than 0")
		}

		var thresholdStr string
		if msg.PolicyType == PolicyTypePercentage {
			thresholdStr = msg.VoteThreshold.String()
		} else {
			thresholdStr = msg.VoteThreshold.TruncateInt().String()
		}

		decPolicy := types.DecisionPolicy{
			PolicyType:         msg.PolicyType,
			Threshold:          thresholdStr,
			VotingPeriod:       int64(msg.VotingPeriod),
			MinExecutionPeriod: int64(msg.MinExecutionPeriod),
		}
		if err := k.DecisionPolicies.Set(ctx, group.PolicyAddress, decPolicy); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to update decision policy for %s", msg.GroupName)
		}

		// Bump policy version to invalidate pending proposals under old rules
		if _, err := k.BumpPolicyVersion(ctx, group.PolicyAddress); err != nil {
			return nil, err
		}
	}

	if err := k.Groups.Set(ctx, msg.GroupName, group); err != nil {
		return nil, err
	}

	return &types.MsgUpdateGroupConfigResponse{}, nil
}

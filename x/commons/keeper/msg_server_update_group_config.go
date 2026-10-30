package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
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

	// The parent is throttled by the group's update_cooldown (spec §7);
	// x/gov is not.
	now := ctx.BlockTime().Unix()
	if !isGov && group.UpdateCooldown > 0 && group.LastConfigUpdate > 0 &&
		now < group.LastConfigUpdate+group.UpdateCooldown {
		return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
			"config update cooldown active: wait until %d", group.LastConfigUpdate+group.UpdateCooldown)
	}

	// The x/split share is the community's revenue split: x/gov only.
	if msg.FundingWeight > 0 {
		if !isGov {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "funding_weight can only be changed by x/gov")
		}
		if err := k.splitKeeper.SetShareByAddress(ctx, group.PolicyAddress, msg.FundingWeight); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update split share")
		}
		group.FundingWeight = msg.FundingWeight
	}

	// Ballot mode (UNSPECIFIED leaves it unchanged). A SECRET or
	// PROPOSER_CHOICE group needs equal member weights and enough registered
	// ZK keys at each secret ballot; that is checked when one opens.
	if msg.BallotMode != types.BallotMode_BALLOT_MODE_UNSPECIFIED {
		if _, ok := types.BallotMode_name[int32(msg.BallotMode)]; !ok {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unknown ballot_mode %d", msg.BallotMode)
		}
		group.BallotMode = msg.BallotMode
	}

	// Validate & update fields
	if msg.MaxSpendPerEpoch != nil {
		if msg.MaxSpendPerEpoch.IsNegative() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch cannot be negative")
		}
		coin := sdk.NewCoin(k.BondDenom(ctx), *msg.MaxSpendPerEpoch)
		if !coin.IsValid() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch must be valid")
		}
		// A parent may lower or raise a child's cap only within its own
		// cap, and may never lift it (0 = no cap).
		if !isGov {
			if err := k.checkSpendCapWithinParent(ctx, msg.MaxSpendPerEpoch, group.ParentPolicyAddress); err != nil {
				return nil, err
			}
		}
		group.MaxSpendPerEpoch = msg.MaxSpendPerEpoch
	}

	if msg.UpdateCooldown > 0 {
		group.UpdateCooldown = int64(msg.UpdateCooldown)
	}

	enablingFutarchy := false
	if msg.FutarchyEnabled != nil {
		enablingFutarchy = msg.FutarchyEnabled.Value && !group.FutarchyEnabled
		group.FutarchyEnabled = msg.FutarchyEnabled.Value
	}

	// Size bounds: x/gov only (docs/security-hardening.md), and the
	// standing committees' compiled [2,5] bounds hold even for x/gov.
	minChanged := msg.MinMembers > 0 && msg.MinMembers != group.MinMembers
	maxChanged := msg.MaxMembers > 0 && msg.MaxMembers != group.MaxMembers
	if !isGov && (minChanged || maxChanged) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "min_members and max_members can only be changed by x/gov")
	}
	targetMin := group.MinMembers
	if msg.MinMembers > 0 {
		targetMin = msg.MinMembers
	}
	targetMax := group.MaxMembers
	if msg.MaxMembers > 0 {
		targetMax = msg.MaxMembers
	}
	if targetMin == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidGroupSize, "min_members must be > 0")
	}
	if targetMax < targetMin {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"invalid bounds: max_members (%d) cannot be less than min_members (%d)", targetMax, targetMin)
	}
	if err := checkCommitteeBounds(msg.GroupName, targetMin, targetMax); err != nil {
		return nil, err
	}
	group.MinMembers = targetMin
	group.MaxMembers = targetMax

	if msg.TermDuration > 0 {
		group.TermDuration = int64(msg.TermDuration)
	}

	if msg.ElectoralPolicyAddress != "" && msg.ElectoralPolicyAddress != group.ElectoralPolicyAddress {
		if !isGov {
			if err := k.validateElectoralAddress(ctx, msg.ElectoralPolicyAddress, group.PolicyAddress); err != nil {
				return nil, err
			}
		}
		// The electoral authority sits beside the parent chain; it must not
		// be a descendant of the group, or the group elects itself.
		hasCycle, err := k.DetectCycle(ctx, group.PolicyAddress, msg.ElectoralPolicyAddress)
		if err != nil {
			return nil, err
		}
		if hasCycle {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "electoral_policy_address would make the group its own electoral authority")
		}
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
		if err := k.checkVotingPeriodCap(ctx, int64(msg.VotingPeriod)); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
		}

		members, err := k.GetCouncilMembers(ctx, msg.GroupName)
		if err != nil {
			return nil, err
		}
		if err := validateVoteThreshold(msg.PolicyType, msg.VoteThreshold, totalMemberWeight(members), isGov); err != nil {
			return nil, err
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

	// Every config change invalidates proposals voted under the old config
	// (spec §7), not only decision-policy changes. Bumped at most once.
	if msg.VoteThreshold == nil && msg.PolicyType == "" {
		if _, err := k.BumpPolicyVersion(ctx, group.PolicyAddress); err != nil {
			return nil, err
		}
	}

	if !isGov {
		group.LastConfigUpdate = now
	}
	if err := k.Groups.Set(ctx, msg.GroupName, group); err != nil {
		return nil, err
	}

	// Turning futarchy on schedules the first confidence market; the
	// commons EndBlocker fires it (CMN-08).
	if enablingFutarchy {
		if err := k.MarketTriggerQueue.Set(ctx, collections.Join(now, msg.GroupName)); err != nil {
			return nil, err
		}
	}

	return &types.MsgUpdateGroupConfigResponse{}, nil
}

package keeper

import (
	"context"
	"regexp"
	"time"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Allow alphanumeric, spaces, and hyphens.
var groupNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 \-]*[a-zA-Z0-9]$`)

func (k msgServer) RegisterGroup(goCtx context.Context, msg *types.MsgRegisterGroup) (*types.MsgRegisterGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	govAddress := k.authKeeper.GetModuleAddress(types.GovModuleName).String()
	isGov := (msg.Authority == govAddress)

	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get module params")
	}

	if !isGov && params.ProposalFee != "" {
		fee, err := sdk.ParseCoinsNormalized(params.ProposalFee)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid module param ProposalFee: %s", err)
		}
		if !fee.IsZero() {
			signerAddr, err := sdk.AccAddressFromBech32(msg.Authority)
			if err != nil {
				return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
			}
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, signerAddr, types.ModuleName, fee); err != nil {
				return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "failed to pay registration fee of %s: %s", params.ProposalFee, err)
			}
		}
	}

	// Validation
	nameLen := len(msg.Name)
	if nameLen < 3 || nameLen > 50 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "group name must be between 3 and 50 characters, got %d", nameLen)
	}
	if !groupNameRegex.MatchString(msg.Name) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "group name can only contain alphanumeric characters, spaces, and hyphens")
	}
	if msg.TermDuration <= 0 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "term_duration must be positive, got %d", msg.TermDuration)
	}
	if msg.MinMembers == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "min_members must be greater than 0")
	}
	if msg.MaxMembers < msg.MinMembers {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "max_members (%d) cannot be less than min_members (%d)", msg.MaxMembers, msg.MinMembers)
	}

	initialCount := uint64(len(msg.Members))
	if initialCount < msg.MinMembers {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "initial count %d below min %d", initialCount, msg.MinMembers)
	}
	if initialCount > msg.MaxMembers {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize, "initial count %d exceeds max %d", initialCount, msg.MaxMembers)
	}

	if msg.MaxSpendPerEpoch != nil {
		if msg.MaxSpendPerEpoch.IsNegative() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch cannot be negative")
		}
		coin := sdk.NewCoin("uspark", *msg.MaxSpendPerEpoch)
		if !coin.IsValid() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch must be valid")
		}
	}

	if msg.UpdateCooldown < 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "update_cooldown cannot be negative")
	}
	if msg.PolicyType != PolicyTypePercentage && msg.PolicyType != PolicyTypeThreshold {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid policy_type '%s'", msg.PolicyType)
	}
	if msg.VotingPeriod <= 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "voting_period must be greater than 0")
	}

	if !isGov {
		for _, allowedMsg := range msg.AllowedMessages {
			if RestrictedMessages[allowedMsg] {
				return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %s is restricted to x/gov groups only", allowedMsg)
			}
		}
	}

	// Authority & Hierarchy
	var finalParent string
	if isGov {
		if msg.IntendedParentAddress != "" {
			finalParent = msg.IntendedParentAddress
		} else {
			finalParent = msg.Authority
		}
	} else {
		hasProfile, err := k.PolicyToName.Has(ctx, msg.Authority)
		if err != nil {
			return nil, err
		}
		if !hasProfile {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "signer must be x/gov or a registered council")
		}
		if msg.IntendedParentAddress != "" && msg.IntendedParentAddress != msg.Authority {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only x/gov can assign groups to other parents")
		}
		finalParent = msg.Authority
	}

	// Create native council state
	members, err := k.parseMembers(msg.Members, msg.MemberWeights)
	if err != nil {
		return nil, err
	}

	councilID, err := k.CouncilSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	policyAddr := DeriveCouncilAddress(councilID, "standard")
	policyAddrStr := policyAddr.String()

	for _, m := range members {
		m.AddedAt = ctx.BlockTime().Unix()
		if err := k.AddMember(ctx, msg.Name, m); err != nil {
			return nil, err
		}
	}

	// Store decision policy
	votingPeriod := time.Duration(msg.VotingPeriod) * time.Second
	minExecutionPeriod := time.Duration(msg.MinExecutionPeriod) * time.Second

	var thresholdStr string
	if msg.PolicyType == PolicyTypePercentage {
		thresholdStr = msg.VoteThreshold.String()
	} else {
		thresholdStr = msg.VoteThreshold.TruncateInt().String()
	}

	decPolicy := types.DecisionPolicy{
		PolicyType:         msg.PolicyType,
		Threshold:          thresholdStr,
		VotingPeriod:       int64(votingPeriod.Seconds()),
		MinExecutionPeriod: int64(minExecutionPeriod.Seconds()),
	}
	if err := k.DecisionPolicies.Set(ctx, policyAddrStr, decPolicy); err != nil {
		return nil, err
	}
	if err := k.PolicyVersion.Set(ctx, policyAddrStr, 0); err != nil {
		return nil, err
	}

	// Cycle detection
	hasCycle, err := k.DetectCycle(ctx, policyAddrStr, finalParent)
	if err != nil {
		return nil, err
	}
	if hasCycle {
		return nil, errorsmod.Wrap(types.ErrInvalidGroupSize, "creation would result in a cyclic parent-child relationship")
	}

	// Finalize
	if msg.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, policyAddrStr, msg.FundingWeight)
	}

	if len(msg.AllowedMessages) > 0 {
		if err := k.PolicyPermissions.Set(ctx, policyAddrStr, types.PolicyPermissions{
			PolicyAddress:   policyAddrStr,
			AllowedMessages: msg.AllowedMessages,
		}); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set policy permissions")
		}
	}

	group := types.Group{
		GroupId:                councilID,
		PolicyAddress:          policyAddrStr,
		ParentPolicyAddress:    finalParent,
		ElectoralPolicyAddress: msg.ElectoralPolicyAddress,
		FundingWeight:          msg.FundingWeight,
		MaxSpendPerEpoch:       msg.MaxSpendPerEpoch,
		UpdateCooldown:         int64(msg.UpdateCooldown),
		FutarchyEnabled:        msg.FutarchyEnabled,
		MinMembers:             msg.MinMembers,
		MaxMembers:             msg.MaxMembers,
		TermDuration:           int64(msg.TermDuration),
		CurrentTermExpiration:  ctx.BlockTime().Unix() + int64(msg.TermDuration),
		ActivationTime:         int64(msg.ActivationTime),
		LastParentUpdate:       ctx.BlockTime().Unix(),
	}

	if err := k.Groups.Set(ctx, msg.Name, group); err != nil {
		return nil, err
	}
	if err := k.PolicyToName.Set(ctx, policyAddrStr, msg.Name); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set policy index")
	}

	if msg.FutarchyEnabled {
		if err := k.TriggerGovernanceMarket(ctx, msg.Name); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create initial governance market")
		}
	}

	return &types.MsgRegisterGroupResponse{}, nil
}

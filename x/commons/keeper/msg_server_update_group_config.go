package keeper

import (
	"context"
	"time"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	grouptype "github.com/cosmos/cosmos-sdk/x/group"
)

func (k msgServer) UpdateGroupConfig(goCtx context.Context, msg *types.MsgUpdateGroupConfig) (*types.MsgUpdateGroupConfigResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Retrieve the Group being updated
	group, err := k.ExtendedGroup.Get(ctx, msg.GroupName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "group %s not found", msg.GroupName)
	}

	// 2. AUTHORIZATION CHECK
	// Only the Parent OR x/gov can change the constitution.
	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	isGov := (msg.Authority == govAddr)
	isParent := (msg.Authority == group.ParentPolicyAddress)

	if !isGov && !isParent {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"only the parent (%s) or x/gov can update this config", group.ParentPolicyAddress)
	}

	// ==========================================
	// 3. VALIDATE & UPDATE EXTENDED GROUP FIELDS
	// ==========================================

	// A. Spend Limit
	if msg.MaxSpendPerEpoch != "" {
		coin, err := sdk.ParseCoinNormalized(msg.MaxSpendPerEpoch)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid max_spend_per_epoch format: %s", err)
		}
		if !coin.IsValid() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch must be valid")
		}
		group.MaxSpendPerEpoch = msg.MaxSpendPerEpoch
	}

	// B. Cooldown
	if msg.UpdateCooldown > 0 {
		group.UpdateCooldown = int64(msg.UpdateCooldown)
	}

	// C. Futarchy
	// Check if the pointer wrapper is nil, and if so, extract the value.
	if msg.FutarchyEnabled != nil {
		group.FutarchyEnabled = msg.FutarchyEnabled.Value
	}

	// D. Member Constraints
	targetMin := group.MinMembers
	if msg.MinMembers > 0 {
		targetMin = uint64(msg.MinMembers)
	}

	targetMax := group.MaxMembers
	if msg.MaxMembers > 0 {
		targetMax = uint64(msg.MaxMembers)
	}

	// Validation Logic
	if targetMin == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidGroupSize, "min_members must be > 0")
	}
	if targetMax < targetMin {
		return nil, errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"invalid bounds: max_members (%d) cannot be less than min_members (%d)", targetMax, targetMin)
	}

	// Apply
	group.MinMembers = targetMin
	group.MaxMembers = targetMax

	// E. Term Duration
	if msg.TermDuration > 0 {
		group.TermDuration = int64(msg.TermDuration)
		// Note: The new duration applies to the NEXT term renewal.
	}

	// F. Electoral Policy (Delegation)
	// Allows appointing a new Elections Committee or changing the delegation.
	if msg.ElectoralPolicyAddress != "" {
		// We blindly accept the address here (standard Cosmos pattern),
		// but checking if it's a valid bech32 or a registered group is also possible.
		group.ElectoralPolicyAddress = msg.ElectoralPolicyAddress
	}

	// ==========================================
	// 4. VALIDATE & UPDATE GROUP POLICY
	// ==========================================

	// Only attempt to update the Group Policy if threshold or policy_type is provided
	if msg.VoteThreshold != "" || msg.PolicyType != "" {
		if msg.PolicyType != PolicyTypePercentage && msg.PolicyType != PolicyTypeThreshold {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid policy_type '%s'. Must be '%s' or '%s'", msg.PolicyType, PolicyTypePercentage, PolicyTypeThreshold)
		}
		if msg.VotingPeriod <= 0 {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "voting_period must be greater than 0")
		}

		// --- Construct the new Decision Policy ---
		var decisionPolicy grouptype.DecisionPolicy
		votingPeriod := time.Duration(msg.VotingPeriod) * time.Second
		minExecutionPeriod := time.Duration(msg.MinExecutionPeriod) * time.Second

		if msg.PolicyType == PolicyTypePercentage {
			// Percentage Policy (e.g., "0.51")
			decisionPolicy = grouptype.NewPercentageDecisionPolicy(msg.VoteThreshold, votingPeriod, minExecutionPeriod)
		} else {
			// Threshold Policy (e.g., "3")
			decisionPolicy = &grouptype.ThresholdDecisionPolicy{
				Threshold: msg.VoteThreshold,
				Windows: &grouptype.DecisionPolicyWindows{
					VotingPeriod:       votingPeriod,
					MinExecutionPeriod: minExecutionPeriod,
				},
			}
		}

		policyAny, err := codectypes.NewAnyWithValue(decisionPolicy)
		if err != nil {
			return nil, err
		}

		_, err = k.groupKeeper.UpdateGroupPolicyDecisionPolicy(goCtx, &grouptype.MsgUpdateGroupPolicyDecisionPolicy{
			Admin:              k.GetModuleAddress().String(),
			GroupPolicyAddress: group.PolicyAddress,
			DecisionPolicy:     policyAny,
		})
		if err != nil {
			return nil, errorsmod.Wrapf(err, "failed to update group policy for %s", msg.GroupName)
		}
	}

	// ==========================================
	// 5. SAVE EXTENDED GROUP STATE
	// ==========================================
	if err := k.ExtendedGroup.Set(ctx, msg.GroupName, group); err != nil {
		return nil, err
	}

	return &types.MsgUpdateGroupConfigResponse{}, nil
}

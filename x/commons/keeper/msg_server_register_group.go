package keeper

import (
	"context"
	"regexp"
	"time"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// Allow alphanumeric, spaces, and hyphens.
// Must start and end with an alphanumeric character (prevents leading/trailing spaces/hyphens).
var groupNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 \-]*[a-zA-Z0-9]$`)

func (k msgServer) RegisterGroup(goCtx context.Context, msg *types.MsgRegisterGroup) (*types.MsgRegisterGroupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// ==========================================
	// 0. AUTHORITY CHECK & FEE ENFORCEMENT
	// ==========================================
	govAddress := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	isGov := (msg.Authority == govAddress)

	// FEE DEDUCTION (Anti-Spam)
	// We waive the fee for x/gov because the proposal already paid a deposit to get here.
	// All other entities (e.g., a Council creating a sub-committee) must pay the tax.
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

			// Transfer Fee: Signer -> x/commons Module Account
			// This effectively locks the tokens in the commons treasury (or burns them if you send to null).
			// Here we send to the module account to build the treasury.
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, signerAddr, types.ModuleName, fee); err != nil {
				return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "failed to pay registration fee of %s: %s", params.ProposalFee, err)
			}
		}
	}

	// ==========================================
	// 1. VALIDATION CHECKS
	// ==========================================

	// Name & Params Validation
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

		// Check if it forms a valid coin (positive)
		coin := sdk.NewCoin("uspark", *msg.MaxSpendPerEpoch)
		if !coin.IsValid() {
			return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "max_spend_per_epoch must be valid")
		}
	}

	if msg.UpdateCooldown < 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "update_cooldown cannot be negative")
	}

	// Policy Type Validation
	if msg.PolicyType != PolicyTypePercentage && msg.PolicyType != PolicyTypeThreshold {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid policy_type '%s'", msg.PolicyType)
	}
	if msg.VotingPeriod <= 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "voting_period must be greater than 0")
	}

	// SECURITY CHECK: Prevent "Self-Granting" of Root Permissions
	// Unless the Creator is x/gov itself, they cannot add Restricted Messages.
	if !isGov {
		for _, allowedMsg := range msg.AllowedMessages {
			if RestrictedMessages[allowedMsg] {
				return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %s is restricted to x/gov groups only", allowedMsg)
			}
		}
	}

	// ==========================================
	// 2. AUTHORITY & HIERARCHY LOGIC
	// ==========================================

	var finalParent string

	if isGov {
		if msg.IntendedParentAddress != "" {
			finalParent = msg.IntendedParentAddress
		} else {
			finalParent = msg.Authority
		}
	} else {
		// Non-Gov Signer: Must be an existing Extended Group
		// OPTIMIZED: Use the PolicyToName index for O(1) lookup
		hasProfile, err := k.PolicyToName.Has(ctx, msg.Authority)
		if err != nil {
			return nil, err
		}

		if !hasProfile {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "signer must be x/gov or a registered council")
		}

		// Only x/gov can assign arbitrary parents
		if msg.IntendedParentAddress != "" && msg.IntendedParentAddress != msg.Authority {
			return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only x/gov can assign groups to other parents")
		}
		finalParent = msg.Authority
	}

	// ==========================================
	// 3. EXECUTION
	// ==========================================

	members, err := k.parseMembers(msg.Members, msg.MemberWeights)
	if err != nil {
		return nil, err
	}

	groupRes, err := k.groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    k.GetModuleAddress().String(),
		Members:  members,
		Metadata: msg.Description,
	})
	if err != nil {
		return nil, err
	}

	// Policy Creation
	var decisionPolicy group.DecisionPolicy
	votingPeriod := time.Duration(msg.VotingPeriod) * time.Second
	minExecutionPeriod := time.Duration(msg.MinExecutionPeriod) * time.Second

	if msg.PolicyType == PolicyTypePercentage {
		// Percentage Policy (e.g., "0.51")
		decisionPolicy = group.NewPercentageDecisionPolicy(msg.VoteThreshold.String(), votingPeriod, minExecutionPeriod)
	} else {
		// Threshold Policy (e.g., "3")
		decisionPolicy = &group.ThresholdDecisionPolicy{
			Threshold: msg.VoteThreshold.TruncateInt().String(),
			Windows: &group.DecisionPolicyWindows{
				VotingPeriod:       votingPeriod,
				MinExecutionPeriod: minExecutionPeriod,
			},
		}
	}

	policyAny, err := codectypes.NewAnyWithValue(decisionPolicy)
	if err != nil {
		return nil, err
	}

	policyRes, err := k.groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          k.GetModuleAddress().String(),
		GroupId:        groupRes.GroupId,
		DecisionPolicy: policyAny,
	})
	if err != nil {
		return nil, err
	}

	policyAddr := policyRes.Address

	// ==========================================
	// 4. CYCLE DETECTION
	// ==========================================
	// We verify that the 'finalParent' is not the group itself, and does not create a loop.
	// Note: For a BRAND NEW group, a loop is impossible (it has no children),
	// but this check handles the self-parenting edge case and future-proofs the logic.

	hasCycle, err := k.DetectCycle(ctx, policyAddr, finalParent)
	if err != nil {
		return nil, err
	}
	if hasCycle {
		return nil, errorsmod.Wrap(types.ErrInvalidGroupSize, "creation would result in a cyclic parent-child relationship")
	}

	// ==========================================
	// 5. FINALIZE STORAGE
	// ==========================================

	if msg.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, policyAddr, msg.FundingWeight)
	}

	if len(msg.AllowedMessages) > 0 {
		if err := k.PolicyPermissions.Set(ctx, policyAddr, types.PolicyPermissions{
			PolicyAddress:   policyAddr,
			AllowedMessages: msg.AllowedMessages,
		}); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set policy permissions")
		}
	}

	extendedGroup := types.ExtendedGroup{
		GroupId:                groupRes.GroupId,
		PolicyAddress:          policyAddr,
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

	// A. Save Group
	if err := k.ExtendedGroup.Set(ctx, msg.Name, extendedGroup); err != nil {
		return nil, err
	}

	// B. Save Index (Policy -> Name)
	if err := k.PolicyToName.Set(ctx, policyAddr, msg.Name); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set policy index")
	}

	// AUTOMATION: Start the Confidence Engine (only if futarchy is enabled)
	if msg.FutarchyEnabled {
		if err := k.TriggerGovernanceMarket(ctx, msg.Name); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create initial governance market")
		}
	}

	return &types.MsgRegisterGroupResponse{}, nil
}

package keeper

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) SubmitProposal(goCtx context.Context, msg *types.MsgSubmitProposal) (*types.MsgSubmitProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Validate policy address exists
	councilName, err := k.PolicyToName.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unknown policy address %s", msg.PolicyAddress)
	}

	// 2. Get extended group for term expiration check
	extGroup, err := k.Groups.Get(ctx, councilName)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get group")
	}

	// 3. Check term expiration
	if ctx.BlockTime().Unix() > extGroup.CurrentTermExpiration {
		// Allow only MsgRenewGroup when expired
		for _, anyMsg := range msg.Messages {
			var sdkMsg sdk.Msg
			if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
				return nil, err
			}
			if sdk.MsgTypeURL(sdkMsg) != "/sparkdream.commons.v1.MsgRenewGroup" {
				return nil, errorsmod.Wrapf(
					sdkerrors.ErrUnauthorized,
					"TERM EXPIRED: Group %s expired on %d. You can only submit MsgRenewGroup proposals.",
					councilName, extGroup.CurrentTermExpiration,
				)
			}
		}
	}

	// 4. Check proposer is a member
	isMember, err := k.HasMember(ctx, councilName, msg.Proposer)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s is not a member of %s", msg.Proposer, councilName)
	}

	// 5. Check permissions for proposed messages
	perms, err := k.PolicyPermissions.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "no permissions found for %s", msg.PolicyAddress)
	}

	if len(msg.Messages) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty proposal")
	}

	for _, anyMsg := range msg.Messages {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			return nil, err
		}
		typeURL := sdk.MsgTypeURL(sdkMsg)

		isAllowed := false
		for _, allowedURL := range perms.AllowedMessages {
			if typeURL == allowedURL {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "msg %s not allowed for policy %s", typeURL, msg.PolicyAddress)
		}
	}

	// 6. Get decision policy for timing
	decPolicy, err := k.DecisionPolicies.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrap(err, "decision policy not found")
	}

	// 7. Get current policy version
	policyVersion, err := k.GetPolicyVersion(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, err
	}

	// 8. Create proposal
	proposalID, err := k.ProposalSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	now := ctx.BlockTime().Unix()
	proposal := types.Proposal{
		Id:             proposalID,
		CouncilName:    councilName,
		PolicyAddress:  msg.PolicyAddress,
		Proposer:       msg.Proposer,
		Messages:       msg.Messages,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		SubmitTime:     now,
		VotingDeadline: now + decPolicy.VotingPeriod,
		PolicyVersion:  policyVersion,
		Metadata:       msg.Metadata,
		ExecutionTime:  now + decPolicy.VotingPeriod + decPolicy.MinExecutionPeriod,
	}

	if err := k.Proposals.Set(ctx, proposalID, proposal); err != nil {
		return nil, err
	}

	// 9. Index by council
	if err := k.ProposalsByCouncil.Set(ctx, collections.Join(councilName, proposalID)); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"submit_proposal",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", proposalID)),
			sdk.NewAttribute("council_name", councilName),
			sdk.NewAttribute("proposer", msg.Proposer),
		),
	)

	return &types.MsgSubmitProposalResponse{ProposalId: proposalID}, nil
}

func (k msgServer) VoteProposal(goCtx context.Context, msg *types.MsgVoteProposal) (*types.MsgVoteProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	proposal, err := k.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal %d is not open for voting (status: %s)", msg.ProposalId, proposal.Status)
	}

	// Check voting deadline
	if ctx.BlockTime().Unix() > proposal.VotingDeadline {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "voting period has ended for proposal %d", msg.ProposalId)
	}

	// Check voter is a member
	isMember, err := k.HasMember(ctx, proposal.CouncilName, msg.Voter)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s is not a member of %s", msg.Voter, proposal.CouncilName)
	}

	// Store vote (overwrites previous vote)
	vote := types.Vote{
		Voter:      msg.Voter,
		Option:     msg.Option,
		Metadata:   msg.Metadata,
		SubmitTime: ctx.BlockTime().Unix(),
	}
	if err := k.Votes.Set(ctx, collections.Join(msg.ProposalId, msg.Voter), vote); err != nil {
		return nil, err
	}

	// Check if threshold is met (early acceptance)
	accepted, err := k.checkThreshold(ctx, proposal)
	if err != nil {
		return nil, err
	}
	if accepted {
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
		// For early acceptance, set execution_time to now + min_execution_period
		// instead of waiting for the full voting period to elapse.
		decPolicy, policyErr := k.DecisionPolicies.Get(ctx, proposal.PolicyAddress)
		if policyErr == nil {
			proposal.ExecutionTime = ctx.BlockTime().Unix() + decPolicy.MinExecutionPeriod
		}
		if err := k.Proposals.Set(ctx, msg.ProposalId, proposal); err != nil {
			return nil, err
		}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"vote_proposal",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", msg.ProposalId)),
			sdk.NewAttribute("voter", msg.Voter),
			sdk.NewAttribute("option", msg.Option.String()),
		),
	)

	return &types.MsgVoteProposalResponse{}, nil
}

func (k msgServer) ExecuteProposal(goCtx context.Context, msg *types.MsgExecuteProposal) (*types.MsgExecuteProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	proposal, err := k.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal %d is not accepted (status: %s)", msg.ProposalId, proposal.Status)
	}

	// Check min execution period
	if ctx.BlockTime().Unix() < proposal.ExecutionTime {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"min execution period has not elapsed (execution_time: %d, current: %d)",
			proposal.ExecutionTime, ctx.BlockTime().Unix())
	}

	// Check policy version hasn't changed (veto check)
	currentVersion, err := k.GetPolicyVersion(ctx, proposal.PolicyAddress)
	if err != nil {
		return nil, err
	}
	if currentVersion != proposal.PolicyVersion {
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_VETOED
		proposal.FailedReason = "policy version changed (vetoed)"
		_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"proposal %d was invalidated by a policy version change (veto)", msg.ProposalId)
	}

	// Check term expiration
	councilName := proposal.CouncilName
	extGroup, err := k.Groups.Get(ctx, councilName)
	if err == nil && ctx.BlockTime().Unix() > extGroup.CurrentTermExpiration {
		// Allow MsgRenewGroup execution even when expired
		allowExecution := true
		for _, anyMsg := range proposal.Messages {
			var sdkMsg sdk.Msg
			if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
				allowExecution = false
				break
			}
			if sdk.MsgTypeURL(sdkMsg) != "/sparkdream.commons.v1.MsgRenewGroup" {
				allowExecution = false
				break
			}
		}
		if !allowExecution {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
				"TERM EXPIRED: Group %s expired on %d. Cannot execute pending proposal %d.",
				councilName, extGroup.CurrentTermExpiration, msg.ProposalId)
		}
	}

	// Execute messages as the policy address via msg router
	if len(proposal.Messages) > 0 && k.late.router == nil {
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FAILED
		proposal.FailedReason = "msg router not wired"
		_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "msg router not wired into commons keeper")
	}

	for i, anyMsg := range proposal.Messages {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FAILED
			proposal.FailedReason = fmt.Sprintf("failed to unpack message %d: %v", i, err)
			_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
			return nil, err
		}

		// SECURITY: Verify that the message's authority/signer matches the
		// proposal's policy address. This prevents privilege escalation where
		// a malicious proposer crafts messages with Authority set to a
		// higher-privilege address (e.g., x/gov).
		if err := k.validateMsgAuthority(sdkMsg, proposal.PolicyAddress); err != nil {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FAILED
			proposal.FailedReason = fmt.Sprintf("message %d authority mismatch: %v", i, err)
			_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "message %d: %v", i, err)
		}

		handler := k.late.router.Handler(sdkMsg)
		if handler == nil {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FAILED
			proposal.FailedReason = fmt.Sprintf("no handler for message %d: %s", i, sdk.MsgTypeURL(sdkMsg))
			_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "no handler for %s", sdk.MsgTypeURL(sdkMsg))
		}

		_, err = handler(ctx, sdkMsg)
		if err != nil {
			proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_FAILED
			proposal.FailedReason = fmt.Sprintf("message %d execution failed: %v", i, err)
			_ = k.Proposals.Set(ctx, msg.ProposalId, proposal)
			return nil, errorsmod.Wrapf(err, "message %d execution failed", i)
		}
	}

	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_EXECUTED
	if err := k.Proposals.Set(ctx, msg.ProposalId, proposal); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"execute_proposal",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", msg.ProposalId)),
			sdk.NewAttribute("executor", msg.Executor),
			sdk.NewAttribute("status", "EXECUTED"),
		),
	)

	return &types.MsgExecuteProposalResponse{}, nil
}

// checkThreshold checks if a proposal has met its decision policy threshold.
// Includes both regular council votes (weighted) and anonymous votes (weight=1 each).
//
// DESIGN NOTE (COMMONS-7): Anonymous votes (via x/shield) are counted with weight=1 each
// and are added to both the numerator (if YES) and denominator of percentage-based thresholds.
// This intentionally dilutes council member weighted votes, reflecting a democratic design
// choice: anonymous community participation can influence proposal outcomes proportionally.
// This is the intended behavior for proposals that enable anonymous voting — it allows
// broader democratic participation while maintaining the council's structural advantage
// through weighted votes.
func (k Keeper) checkThreshold(ctx context.Context, proposal types.Proposal) (bool, error) {
	decPolicy, err := k.DecisionPolicies.Get(ctx, proposal.PolicyAddress)
	if err != nil {
		return false, err
	}

	// Tally regular (council member) votes
	var yesWeight, totalWeight math.LegacyDec
	yesWeight = math.LegacyZeroDec()
	totalWeight = math.LegacyZeroDec()

	rng := collections.NewPrefixedPairRange[uint64, string](proposal.Id)
	err = k.Votes.Walk(ctx, rng, func(_ collections.Pair[uint64, string], vote types.Vote) (bool, error) {
		// Get member weight
		member, err := k.GetMember(ctx, proposal.CouncilName, vote.Voter)
		if err != nil {
			return false, nil // Member may have been removed since voting
		}

		weight, err := math.LegacyNewDecFromStr(member.Weight)
		if err != nil {
			return false, nil
		}

		totalWeight = totalWeight.Add(weight)
		if vote.Option == types.VoteOption_VOTE_OPTION_YES {
			yesWeight = yesWeight.Add(weight)
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}

	// Include anonymous votes (weight=1 each)
	anonTally, anonErr := k.AnonVoteTallies.Get(ctx, proposal.Id)
	if anonErr == nil {
		yesDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.YesCount))
		noDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.NoCount))
		abstainDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.AbstainCount))
		vetoDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.NoWithVetoCount))
		anonTotalDec := yesDec.Add(noDec).Add(abstainDec).Add(vetoDec)
		yesWeight = yesWeight.Add(yesDec)
		totalWeight = totalWeight.Add(anonTotalDec)
	}

	if totalWeight.IsZero() {
		return false, nil
	}

	threshold, err := math.LegacyNewDecFromStr(decPolicy.Threshold)
	if err != nil {
		return false, nil
	}

	if decPolicy.PolicyType == "percentage" {
		// Percentage: yesWeight / (totalGroupWeight + anonTotal) >= threshold
		allMembers, err := k.GetCouncilMembers(ctx, proposal.CouncilName)
		if err != nil {
			return false, err
		}
		groupWeight := math.LegacyZeroDec()
		for _, m := range allMembers {
			w, err := math.LegacyNewDecFromStr(m.Weight)
			if err != nil {
				continue
			}
			groupWeight = groupWeight.Add(w)
		}
		// Add anonymous votes to the denominator
		if anonErr == nil {
			yesDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.YesCount))
			noDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.NoCount))
			abstainDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.AbstainCount))
			vetoDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(anonTally.NoWithVetoCount))
			anonTotalDec := yesDec.Add(noDec).Add(abstainDec).Add(vetoDec)
			groupWeight = groupWeight.Add(anonTotalDec)
		}
		if groupWeight.IsZero() {
			return false, nil
		}
		ratio := yesWeight.Quo(groupWeight)
		return ratio.GTE(threshold), nil
	}

	// Threshold: yesWeight >= threshold
	thresholdInt, err := strconv.ParseUint(decPolicy.Threshold, 10, 64)
	if err != nil {
		return false, nil
	}
	return yesWeight.GTE(math.LegacyNewDec(int64(thresholdInt))), nil
}

// validateMsgAuthority checks that every signer declared on a proposal message
// (via the proto `cosmos.msg.v1.signer` option) matches the proposal's policy
// address. This prevents privilege escalation where a malicious proposer crafts
// messages with the signer set to a higher-privilege address (e.g., x/gov).
//
// Uses the SDK signing context directly rather than reflection so the check
// works uniformly across every signer field name (authority, voter, proposer,
// creator, submitter, staker, juror, ...).
func (k Keeper) validateMsgAuthority(sdkMsg sdk.Msg, policyAddress string) error {
	signers, _, err := k.cdc.GetMsgV1Signers(sdkMsg)
	if err != nil {
		return fmt.Errorf("could not extract signers from %s: %w", sdk.MsgTypeURL(sdkMsg), err)
	}
	if len(signers) == 0 {
		return fmt.Errorf("message %s has no declared signer", sdk.MsgTypeURL(sdkMsg))
	}

	policyBytes, err := k.addressCodec.StringToBytes(policyAddress)
	if err != nil {
		return fmt.Errorf("invalid policy address %s: %w", policyAddress, err)
	}

	for i, s := range signers {
		if !bytes.Equal(s, policyBytes) {
			return fmt.Errorf("signer[%d] %s does not match policy address %s",
				i, sdk.AccAddress(s).String(), policyAddress)
		}
	}
	return nil
}

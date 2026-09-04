package keeper

import (
	"context"
	"fmt"
	stdmath "math"
	"strings"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateInitiative creates a new initiative under a project
func (k Keeper) CreateInitiative(
	ctx context.Context,
	creator sdk.AccAddress,
	projectID uint64,
	title, description string,
	tags []string,
	tier types.InitiativeTier,
	category types.InitiativeCategory,
	budget math.Int,
	acceptanceCriteria ...types.VerificationCriteria,
) (uint64, error) {
	// Validate project exists and is active
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return 0, fmt.Errorf("project must be in ACTIVE status")
	}

	// Get params for tier validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get params: %w", err)
	}

	// Permissionless projects: enforce tier cap
	if project.Permissionless {
		if uint32(tier) > params.PermissionlessMaxTier {
			return 0, fmt.Errorf("tier %s exceeds permissionless max tier: %w", tier.String(), types.ErrPermissionlessTierExceeded)
		}

		// Validate trust level for initiative creation under permissionless projects
		member, mErr := k.Member.Get(ctx, creator.String())
		if mErr != nil {
			return 0, fmt.Errorf("failed to get member: %w", mErr)
		}
		// APPRENTICE requires PROVISIONAL+, STANDARD requires ESTABLISHED+
		var requiredTrust uint32
		if tier == types.InitiativeTier_INITIATIVE_TIER_STANDARD {
			requiredTrust = params.PermissionlessMinTrustLevel // ESTABLISHED
		} else {
			// APPRENTICE: one level below PermissionlessMinTrustLevel (min 1 = PROVISIONAL)
			if params.PermissionlessMinTrustLevel > 1 {
				requiredTrust = params.PermissionlessMinTrustLevel - 1
			} else {
				requiredTrust = 1 // At least PROVISIONAL
			}
		}
		if uint32(member.TrustLevel) < requiredTrust {
			return 0, fmt.Errorf("trust level %d below required %d for %s tier in permissionless project: %w",
				member.TrustLevel, requiredTrust, tier.String(), types.ErrInsufficientTrustLevel)
		}

		// Burn initiative creation fee
		var fee math.Int
		switch tier {
		case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
			fee = params.InitiativeCreationFeeApprentice
		case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
			fee = params.InitiativeCreationFeeStandard
		default:
			fee = params.InitiativeCreationFeeStandard // fallback (shouldn't happen due to tier cap)
		}
		if fee.IsPositive() {
			if err := k.BurnDREAM(ctx, creator, fee); err != nil {
				return 0, fmt.Errorf("failed to burn initiative creation fee: %w", types.ErrInsufficientCreationFee)
			}
		}
	}

	// Validate budget is within tier limits
	var tierConfig types.TierConfig
	var tierName string
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
		tierName = "apprentice"
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
		tierName = "standard"
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
		tierName = "expert"
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
		tierName = "epic"
	default:
		return 0, fmt.Errorf("invalid initiative tier: %v", tier)
	}

	// A negative budget passed every check below: AllocateBudget tests
	// `available < amount`, which is never true for a negative, so
	// allocated_budget SHRANK and the project's future headroom grew past what
	// the committee approved. The initiative itself could never complete
	// (MintDREAM rejects negatives), but the accounting damage was done.
	if budget.IsNil() || budget.IsNegative() {
		return 0, errorsmod.Wrapf(types.ErrInvalidAmount,
			"initiative budget cannot be negative: %s", budget)
	}

	if budget.GT(tierConfig.MaxBudget) {
		// Convert micro-DREAM to DREAM for readable error (1 DREAM = 1,000,000 micro-DREAM)
		budgetDream := budget.Quo(math.NewInt(1000000))
		maxDream := tierConfig.MaxBudget.Quo(math.NewInt(1000000))
		return 0, fmt.Errorf("budget %s DREAM exceeds %s tier maximum of %s DREAM", budgetDream.String(), tierName, maxDream.String())
	}

	// The definition of done is fixed here and never again — pre-commitment is
	// the entire value, since criteria agreed after the work is submitted are
	// just the author marking their own homework.
	if err := ValidateAcceptanceCriteria(acceptanceCriteria); err != nil {
		return 0, err
	}

	// Enforce max tags per initiative (anti-gaming: prevents tag stuffing for rep/revenue inflation)
	if params.MaxTagsPerInitiative > 0 && uint32(len(tags)) > params.MaxTagsPerInitiative {
		return 0, fmt.Errorf("initiative has %d tags, max allowed is %d: %w", len(tags), params.MaxTagsPerInitiative, types.ErrTooManyTags)
	}

	// Validate tags against tag registry (anti-gaming: prevents rep farming in fake tags)
	for _, tag := range tags {
		exists, err := k.TagExists(ctx, tag)
		if err != nil {
			return 0, fmt.Errorf("failed to validate tag %q: %w", tag, err)
		}
		if !exists {
			return 0, fmt.Errorf("tag %q: %w", tag, types.ErrTagNotRegistered)
		}
	}

	// Allocate budget from project (skip for permissionless — no pre-allocated budget)
	if !project.Permissionless {
		if err := k.AllocateBudget(ctx, projectID, budget); err != nil {
			return 0, fmt.Errorf("failed to allocate budget: %w", err)
		}
	}

	// Permissionless work must pay for the review its own minting consumes.
	//
	// A permissionless budget is a self-declared number with no treasury behind
	// it, and the review fee it eventually pays is minted — so reviewers of
	// permissionless work would otherwise be funded purely by dilution, the
	// exact outcome the funded path's budget-netting exists to prevent. A
	// creator-funded bounty in EXISTING DREAM prices that attention onto
	// whoever consumes it, and scales the spam brake with the amount being
	// minted rather than leaving it at a flat creation fee.
	// Only charged when the initiative is actually GATED. The bounty pays for
	// mandatory review; below review_required_above_budget no review is
	// required, so charging for it would take DREAM for a service that is never
	// delivered. That threshold equals the APPRENTICE ceiling, so apprentice
	// work — the on-ramp, reachable at PROVISIONAL, and where members arrive
	// holding zero DREAM — carries no bounty at all and still costs only its
	// 1 DREAM creation fee.
	minBounty := math.ZeroInt()
	gated := !params.ReviewRequiredAboveBudget.IsNil() &&
		params.ReviewRequiredAboveBudget.IsPositive() &&
		budget.GT(params.ReviewRequiredAboveBudget)
	if project.Permissionless && gated &&
		!params.PermissionlessMinReviewBountyRate.IsNil() &&
		params.PermissionlessMinReviewBountyRate.IsPositive() {
		minBounty = params.PermissionlessMinReviewBountyRate.MulInt(budget).TruncateInt()
	}

	// Get next initiative ID
	initiativeID, err := k.InitiativeSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next initiative ID: %w", err)
	}

	// Calculate required conviction based on budget and conviction_per_dream parameter
	// (params already fetched above for tier validation)
	// Formula: required_conviction = conviction_per_dream × sqrt(budget_micro_amount)
	// This scales the same way as actual conviction (which uses sqrt dampening)
	// Maintains constant stake-to-budget ratio across all budget sizes
	//
	// IMPORTANT: We take sqrt of the integer value first, then convert to Dec
	// because LegacyDec.ApproxSqrt() operates on the internal representation (value × 10^18)
	// which would give us sqrt(budget × 10^18) = sqrt(budget) × 10^9, which is wrong
	budgetFloat := budget.BigInt().Uint64()
	sqrtBudgetFloat := stdmath.Sqrt(float64(budgetFloat))
	sqrtBudget := math.LegacyMustNewDecFromStr(fmt.Sprintf("%.18f", sqrtBudgetFloat))
	requiredConviction := params.ConvictionPerDream.Mul(sqrtBudget)

	// Create initiative
	initiative := types.Initiative{
		Id:                    initiativeID,
		ProjectId:             projectID,
		Title:                 title,
		Description:           description,
		Tags:                  tags,
		Tier:                  tier,
		Category:              category,
		AcceptanceCriteria:    acceptanceCriteria,
		Budget:                PtrInt(budget),
		RequiredConviction:    PtrDec(requiredConviction),
		CurrentConviction:     PtrDec(math.LegacyZeroDec()),
		ExternalConviction:    PtrDec(math.LegacyZeroDec()),
		ConvictionLastUpdated: sdk.UnwrapSDKContext(ctx).BlockHeight(),
		Status:                types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		CreatedAt:             sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		Creator:               creator.String(),
	}

	// Store initiative
	if err := k.Initiative.Set(ctx, initiativeID, initiative); err != nil {
		return 0, fmt.Errorf("failed to store initiative: %w", err)
	}

	// Add to status index for efficient EndBlocker lookups
	if err := k.AddInitiativeToStatusIndex(ctx, initiative); err != nil {
		return 0, fmt.Errorf("failed to add initiative to status index: %w", err)
	}

	// Escrow the mandatory permissionless bounty. Done after the initiative
	// exists so the escrow has something to attach to, and it fails the whole
	// creation if the creator cannot cover it — which is the point: the brake
	// is meant to bite at creation, not to be discovered later.
	if minBounty.IsPositive() {
		if _, err := k.EscrowReviewBounty(ctx, creator, initiativeID, minBounty); err != nil {
			return 0, fmt.Errorf("permissionless initiative requires a review bounty of %s: %w", minBounty, err)
		}
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_created",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("creator", creator.String()),
			sdk.NewAttribute("tier", tier.String()),
			sdk.NewAttribute("budget", budget.String()),
		),
	)

	return initiativeID, nil
}

// CountActiveInitiativesForAssignee returns the number of in-flight initiatives
// assigned to the given member. "In flight" is every status enumerated by
// IterateActiveInitiatives (OPEN..CHALLENGED). OPEN initiatives with no
// assignee yet are skipped, so the cap only fires on work actually held.
func (k Keeper) CountActiveInitiativesForAssignee(ctx context.Context, assignee string) (uint32, error) {
	if assignee == "" {
		return 0, nil
	}
	var count uint32
	k.IterateActiveInitiatives(ctx, func(_ int64, initiative types.Initiative) bool {
		if initiative.Assignee == assignee {
			count++
		}
		return false
	})
	return count, nil
}

// GetInitiative retrieves an initiative by ID
func (k Keeper) GetInitiative(ctx context.Context, initiativeID uint64) (types.Initiative, error) {
	initiative, err := k.Initiative.Get(ctx, initiativeID)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Initiative{}, fmt.Errorf("initiative %d not found", initiativeID)
		}
		return types.Initiative{}, err
	}
	return initiative, nil
}

// UpdateInitiative updates an existing initiative and maintains the status index
func (k Keeper) UpdateInitiative(ctx context.Context, initiative types.Initiative) error {
	// Get old initiative to detect status changes
	oldInitiative, err := k.Initiative.Get(ctx, initiative.Id)
	if err == nil && oldInitiative.Status != initiative.Status {
		// Status changed - update the index
		if err := k.UpdateInitiativeStatusIndex(ctx, oldInitiative.Status, initiative.Status, initiative.Id); err != nil {
			return fmt.Errorf("failed to update initiative status index: %w", err)
		}
	}

	return k.Initiative.Set(ctx, initiative.Id, initiative)
}

// AssignInitiativeToMember assigns an initiative to a member
func (k Keeper) AssignInitiativeToMember(
	ctx context.Context,
	initiativeID uint64,
	assignee sdk.AccAddress,
) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate status
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
		return fmt.Errorf("initiative must be in OPEN status")
	}

	// Get member to validate tier qualification
	member, err := k.GetMember(ctx, assignee)
	if err != nil {
		return fmt.Errorf("assignee is not a member: %w", err)
	}

	// Validate member is qualified for tier
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Enforce per-member active initiative cap (anti-monopolization).
	if params.MaxActiveInitiativesPerMember > 0 {
		active, cerr := k.CountActiveInitiativesForAssignee(ctx, assignee.String())
		if cerr != nil {
			return fmt.Errorf("failed to count active initiatives: %w", cerr)
		}
		if active >= params.MaxActiveInitiativesPerMember {
			return types.ErrTooManyActiveInitiatives
		}
	}

	var tierConfig types.TierConfig
	switch initiative.Tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
	}

	// Check reputation for initiative tags
	totalRep := math.LegacyZeroDec()
	for _, tag := range initiative.Tags {
		if repStr, ok := member.ReputationScores[tag]; ok {
			rep, err := math.LegacyNewDecFromStr(repStr)
			if err == nil {
				totalRep = totalRep.Add(rep)
			}
		}
	}

	// Calculate average reputation - handle case where initiative has no tags
	var avgRep math.LegacyDec
	if len(initiative.Tags) > 0 {
		avgRep = totalRep.QuoInt64(int64(len(initiative.Tags)))
	} else {
		// No tags - calculate average from all reputation scores
		if len(member.ReputationScores) > 0 {
			totalAllRep := math.LegacyZeroDec()
			for _, repStr := range member.ReputationScores {
				rep, err := math.LegacyNewDecFromStr(repStr)
				if err == nil {
					totalAllRep = totalAllRep.Add(rep)
				}
			}
			avgRep = totalAllRep.QuoInt64(int64(len(member.ReputationScores)))
		} else {
			avgRep = math.LegacyZeroDec()
		}
	}

	if avgRep.LT(tierConfig.MinReputation) {
		return fmt.Errorf("insufficient reputation for tier: have %s, need %s", avgRep.String(), tierConfig.MinReputation.String())
	}

	// Self-assignment bond: taking an initiative you commissioned yourself
	// locks a fraction of its budget as a DREAM bond — returned on
	// completion/abandonment, burned on upheld challenge.
	//
	// Permissionless projects used to be exempt on the grounds that they carry
	// no treasury exposure. That reads the exposure backwards. A budget-backed
	// initiative MOVES DREAM a council already approved; a permissionless one
	// MINTS DREAM nobody approved, and the dilution lands on every holder.
	// Since CompleterShare + TreasuryShare == 1, the budget IS the mint, so the
	// bond base is already right — what the minting case needs is the heavier
	// rate, not a different base.
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return err
	}
	if IsSelfAssigned(initiative, project, assignee.String()) {
		bondRate := params.SelfAssignedBondRate
		if project.Permissionless {
			bondRate = params.PermissionlessSelfAssignedBondRate
		}
		if bondRate.IsPositive() {
			bond := DerefInt(initiative.Budget).ToLegacyDec().Mul(bondRate).TruncateInt()
			if bond.IsPositive() {
				if err := k.LockDREAM(ctx, assignee, bond); err != nil {
					return fmt.Errorf("failed to lock self-assign bond of %s DREAM: %w", bond, err)
				}
				initiative.SelfAssignBond = PtrInt(bond)
			}
		}
	}

	// Update initiative
	initiative.Assignee = assignee.String()
	initiative.AssignedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_assigned",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", assignee.String()),
		),
	)

	return nil
}

// SubmitInitiativeWork submits work for review
func (k Keeper) SubmitInitiativeWork(
	ctx context.Context,
	initiativeID uint64,
	assignee sdk.AccAddress,
	deliverableURI string,
) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate assignee
	if initiative.Assignee != assignee.String() {
		return fmt.Errorf("only assignee can submit work")
	}

	// Validate status
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED {
		return fmt.Errorf("initiative must be in ASSIGNED status")
	}

	// Require something to review. Nothing on the happy path reads the
	// deliverable — completion turns on conviction — so an empty URI submitted
	// here rides through the review window, past the challenge window and into
	// a payout, having given stakers, challengers and jurors nothing to judge.
	// Enforced keeper-side rather than in a ValidateBasic: SDK 0.50+ deprecates
	// ValidateBasic and this module has none anywhere in x/rep/types.
	deliverableURI = strings.TrimSpace(deliverableURI)
	if deliverableURI == "" {
		return types.ErrEmptyDeliverable
	}
	if len(deliverableURI) > types.MaxDeliverableURILength {
		return fmt.Errorf("%w: deliverable URI is %d characters, max %d",
			types.ErrInvalidRequest, len(deliverableURI), types.MaxDeliverableURILength)
	}

	// Get params for review periods
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Update initiative
	initiative.DeliverableUri = deliverableURI
	initiative.SubmittedAt = sdkCtx.BlockTime().Unix()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	initiative.ReviewPeriodEnd = currentHeight + (params.DefaultReviewPeriodEpochs * params.EpochBlocks)

	// Open a reviewer window for this round when the parent project asks for
	// sign-off. Past the deadline with the gate unmet, the EndBlocker escalates
	// to the Operations Committee rather than letting the work sit forever.
	if project, pErr := k.GetProject(ctx, initiative.ProjectId); pErr == nil &&
		ReviewRequired(params, initiative, project) {
		reviewEpochs := params.DefaultReviewPeriodEpochs
		// The policy may be nil: the chain-wide budget threshold gates an
		// initiative whether or not its project declared a policy of its own.
		if project.VerificationPolicy != nil && project.VerificationPolicy.ReviewPeriodEpochs > reviewEpochs {
			reviewEpochs = project.VerificationPolicy.ReviewPeriodEpochs
		}
		initiative.ReviewDeadline = currentHeight + (reviewEpochs * params.EpochBlocks)
		initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
		// Snapshot the project-policy requirement so it cannot be relaxed
		// mid-flight. The chain-wide threshold is deliberately NOT snapshotted
		// — see RequiredVerifiersFor.
		if project.VerificationPolicy != nil {
			initiative.RequiredVerifiers = project.VerificationPolicy.MinVerifierCount
		} else {
			initiative.RequiredVerifiers = 0
		}
	} else {
		initiative.RequiredVerifiers = 0
	}

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_work_submitted",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", assignee.String()),
			sdk.NewAttribute("deliverable_uri", deliverableURI),
		),
	)

	return nil
}

// ReleaseSelfAssignBond unlocks a held self-assign bond back to the assignee
// and clears it on the passed initiative. No-op when no bond is held. The
// caller is responsible for persisting the initiative afterwards.
func (k Keeper) ReleaseSelfAssignBond(ctx context.Context, initiative *types.Initiative) error {
	bond := DerefInt(initiative.SelfAssignBond)
	if !bond.IsPositive() {
		return nil
	}
	assigneeAddr, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return fmt.Errorf("invalid assignee address: %w", err)
	}
	if err := k.UnlockDREAM(ctx, assigneeAddr, bond); err != nil {
		return fmt.Errorf("failed to unlock self-assign bond: %w", err)
	}
	initiative.SelfAssignBond = PtrInt(math.ZeroInt())
	return nil
}

// BurnSelfAssignBond burns a held self-assign bond (upheld-challenge slash)
// and clears it on the passed initiative. No-op when no bond is held. The
// caller is responsible for persisting the initiative afterwards.
func (k Keeper) BurnSelfAssignBond(ctx context.Context, initiative *types.Initiative) error {
	bond := DerefInt(initiative.SelfAssignBond)
	if !bond.IsPositive() {
		return nil
	}
	assigneeAddr, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return fmt.Errorf("invalid assignee address: %w", err)
	}
	if err := k.UnlockDREAM(ctx, assigneeAddr, bond); err != nil {
		return fmt.Errorf("failed to unlock self-assign bond: %w", err)
	}
	if err := k.BurnDREAM(ctx, assigneeAddr, bond); err != nil {
		return fmt.Errorf("failed to burn self-assign bond: %w", err)
	}
	initiative.SelfAssignBond = PtrInt(math.ZeroInt())
	return nil
}

// UnassignInitiative releases an assignment and returns the initiative to OPEN
// so someone else can pick it up.
//
// This is not a way out of the lifecycle. Conviction, its stakes and the
// funding all stay attached to the initiative, which keeps accruing conviction
// because OPEN is an active status (see IterateActiveInitiatives). That is the
// point: the demand the community staked on the work is a property of the work,
// not of whoever happened to be holding it, and destroying it on a change of
// hands would make stakers pay for someone else stepping down. Retiring the
// item outright is CloseInitiative.
//
// Status rules, and why they differ for a forced release:
//
//   - ASSIGNED is always releasable. The current round holds no verdicts there
//     (a rejection increments the round before returning to ASSIGNED), so
//     nothing is owed to a reviewer and nothing is minted.
//   - SUBMITTED and IN_REVIEW are releasable only by the Operations Committee.
//     Verdicts can be filed in either state, so a self-service release here
//     would let an assignee submit, draw reviewer effort, release, and resubmit
//     on a fresh round — minting review fees each lap at no cost to anyone but
//     the token supply. Behind the committee that loop is not self-serve.
//   - CHALLENGED is never releasable, by anyone. Walking away from a live
//     challenge and re-entering through a new assignee would launder it.
//
// Review bonds committed against rounds now being discarded are released: those
// reviewers judged a submission that is being withdrawn, and holding their bond
// hostage to a future assignee's conduct would charge them for someone else's
// exit. No fee is paid for those rounds, matching what already happens to a
// superseded round after a rejection. Any review bounty stays funded and in
// escrow, since the initiative it was posted against is still live.
func (k Keeper) UnassignInitiative(
	ctx context.Context,
	initiativeID uint64,
	reason string,
	forced bool,
) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	if initiative.Assignee == "" {
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d has no assignee to release", initiativeID)
	}

	// The two failure kinds are deliberately different errors. A status nobody
	// can release from is ErrInvalidInitiativeStatus; a status only the
	// committee can release from is ErrUnauthorized, because the same call from
	// a different signer succeeds. A client can tell "wait" from "ask someone
	// else" without parsing the message.
	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED:
	case types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW:
		if !forced {
			return errorsmod.Wrapf(types.ErrUnauthorized,
				"initiative %d is under review; see the round out or ask the operations committee to release it",
				initiativeID)
		}
	case types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED:
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d has an open challenge and cannot be released until it resolves", initiativeID)
	default:
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d cannot be released from status %s", initiativeID, initiative.Status.String())
	}

	// Release every bond still committed against a verdict. Nothing is minted
	// here, so this cannot be pumped by releasing repeatedly.
	if err := k.SettleReviewBonds(ctx, initiativeID); err != nil {
		return fmt.Errorf("failed to settle review bonds: %w", err)
	}

	// The self-assign bond guards against an upheld challenge on work that
	// paid out. Nothing paid out and nothing is contestable, so it goes back.
	if err := k.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
		return err
	}

	// Drop any escalation flag along with the round it belonged to. Left set,
	// the sweep would treat a later round as already with the committee and
	// that round would silently lose its escalation path.
	if err := k.EscalatedReviews.Remove(ctx, initiativeID); err != nil {
		return err
	}

	previous := initiative.Assignee

	// Everything tied to who was holding the work, cleared. Budget, conviction
	// and acceptance criteria are properties of the initiative and stay put.
	initiative.Assignee = ""
	initiative.Apprentice = ""
	initiative.AssignedAt = 0
	initiative.DeliverableUri = ""
	initiative.SubmittedAt = 0
	initiative.Approvals = nil
	// The round counter is never reset. Verdict records are keyed
	// (initiative, round, reviewer) and outlive the assignment, so pointing the
	// next submission back at round 0 would hand it the previous holder's
	// verdicts: stale approvals would satisfy the review gate for work nobody
	// looked at, PayReviewFees would mint to their authors a second time, and
	// those authors would be locked out of filing a real verdict.
	//
	// Advance past a round that actually collected verdicts, so the next
	// submission starts on a clean key range. A round with no verdicts is
	// already clean and is left alone: self-assigning and releasing costs
	// nothing but gas, so consuming a round there would let anyone burn an
	// initiative's max_review_rounds budget from the outside.
	if discarded, rErr := k.GetInitiativeReviews(ctx, initiativeID, initiative.ReviewRound); rErr != nil {
		return rErr
	} else if len(discarded) > 0 {
		initiative.ReviewRound++
	}
	initiative.ReviewDeadline = 0
	initiative.ReviewEscalation = types.ReviewEscalation_REVIEW_ESCALATION_NONE
	initiative.RequiredVerifiers = 0
	initiative.ReviewPeriodEnd = 0
	initiative.ChallengePeriodEnd = 0
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_OPEN

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_unassigned",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", previous),
			sdk.NewAttribute("forced", fmt.Sprintf("%t", forced)),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// CloseInitiative retires an initiative and returns its budget to the parent
// project. This is the project side deciding the work is not going to happen,
// and it is terminal.
//
// It is not restricted to untouched listings: a project must be able to stop
// funding work whose assignee has gone silent, without needing that assignee's
// cooperation. Authorization lives in
// the msg server, which admits the project creator and the Operations
// Committee, never the assignee — an assignee's exit is UnassignInitiative,
// which leaves the work available to somebody else.
//
// Any live review round is settled on the way out. Reviewers are paid for the
// round that resolved the initiative whether it completed or closed, because a
// fee that depended on the outcome would rebuild the bias the role exists to
// remove.
//
// A CHALLENGED initiative cannot be closed. The challenge decides whether the
// work was delivered, and closing out from under it would let the project side
// void a challenge that was about to be upheld.
//
// Outstanding conviction stakes are deliberately left in place: RemoveStake has
// no status gate, so stakers can withdraw principal plus accrued rewards at any
// time. Moving to a terminal status drops the initiative out of
// IterateActiveInitiatives, so its conviction simply stops being recomputed.
func (k Keeper) CloseInitiative(ctx context.Context, initiativeID uint64, reason string) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW:
	case types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED:
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d has an open challenge and cannot be closed until it resolves", initiativeID)
	default:
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d is already resolved (status %s)", initiativeID, initiative.Status.String())
	}

	if _, bErr := k.PayReviewBounty(ctx, initiative); bErr != nil {
		return fmt.Errorf("failed to settle review bounty: %w", bErr)
	}
	reviewFees, feeErr := k.PayReviewFees(ctx, initiative)
	if feeErr != nil {
		return fmt.Errorf("failed to pay review fees: %w", feeErr)
	}
	if err := k.SettleReviewBonds(ctx, initiativeID); err != nil {
		return fmt.Errorf("failed to settle review bonds: %w", err)
	}

	// Return budget to project (skip for permissionless — no pre-allocated
	// budget), net of what review cost. The project pays for having had the
	// work evaluated, which is where that cost belongs: returning the full
	// budget would make review free to the party that asked for it and fund the
	// reviewers purely by dilution.
	project, projErr := k.GetProject(ctx, initiative.ProjectId)
	if projErr == nil && !project.Permissionless {
		returned := DerefInt(initiative.Budget).Sub(reviewFees)
		if returned.IsNegative() {
			returned = math.ZeroInt()
		}
		if err := k.ReturnBudget(ctx, initiative.ProjectId, returned); err != nil {
			return fmt.Errorf("failed to return budget: %w", err)
		}
	}

	// Return any self-assign bond — the bond guards against upheld challenges,
	// not against the project retiring the work (no payout occurred).
	if err := k.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
		return err
	}

	// Drop any escalation flag. resolveSilentEscalations walks the escalation
	// keyset rather than the status index, so an entry left behind would time
	// out later and reject a round on an initiative that no longer exists as
	// live work — rejectReviewRound would put it back to ASSIGNED with its
	// budget already returned and its bond already released.
	if err := k.EscalatedReviews.Remove(ctx, initiativeID); err != nil {
		return err
	}

	// Harvest what the stakes accrued while the work was live, BEFORE the flip
	// below — stakeAccruing stops paying on a terminal initiative, so settling
	// after it would silently strand every accrued reward.
	if err := k.settleInitiativeStakes(ctx, initiativeID); err != nil {
		return err
	}

	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CLOSED

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_closed",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", initiative.ProjectId)),
			sdk.NewAttribute("assignee", initiative.Assignee),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// CompleteInitiative completes an initiative and distributes rewards
func (k Keeper) CompleteInitiative(ctx context.Context, initiativeID uint64) error {
	// Get initiative
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Validate status - must be IN_REVIEW, and the challenge window must have
	// closed. Payout is the one irreversible step in the lifecycle (it mints,
	// pays stakers, and deletes the stake records), so it is gated on the
	// window in which anyone can contest the work having actually elapsed.
	//
	// SUBMITTED was accepted here until it became clear that it let the
	// assignee skip that window entirely: submit, wait for the EndBlocker to
	// see the conviction thresholds met, then call MsgCompleteInitiative
	// before a single block of the challenge period had run. The community
	// gets its full DefaultChallengePeriodEpochs (doubled for self-assigned
	// work) to raise a challenge, and this guard is what makes that promise
	// real rather than advisory. TransitionToChallengePeriod is the only way
	// into IN_REVIEW, so it always sets ChallengePeriodEnd before this runs.
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
		return errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative must be in IN_REVIEW status to complete, got %s", initiative.Status)
	}
	if currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight(); currentHeight < initiative.ChallengePeriodEnd {
		return errorsmod.Wrapf(types.ErrChallengePeriodActive,
			"challenge period for initiative %d ends at height %d (current %d)",
			initiativeID, initiative.ChallengePeriodEnd, currentHeight)
	}

	// Refuse to mint a payout under a cancelled parent project. Cancelling a
	// project terminates new payouts from it; the clean exit is CloseInitiative
	// (which returns the assignee's self-assign bond and the reserved budget),
	// applied to every live initiative by the cancel cascade.
	// CanCompleteInitiative enforces the same rule for the EndBlocker
	// transition path — this explicit guard gives the manual completion path a
	// clear error and defends the mint even if that check ever changes.
	parentProject, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return fmt.Errorf("failed to get parent project: %w", err)
	}
	if parentProject.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED {
		return fmt.Errorf("cannot complete initiative %d: parent project %d is cancelled", initiativeID, initiative.ProjectId)
	}

	// Check if completion requirements are met
	canComplete, err := k.CanCompleteInitiative(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("failed to check completion requirements: %w", err)
	}
	if !canComplete {
		return fmt.Errorf("initiative does not meet completion requirements")
	}

	// Get params for reward distribution
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Calculate rewards. Budget splits into the completer payout (CompleterShare)
	// and the protocol's TreasuryShare; both are freshly minted DREAM so the
	// total mint equals the budget when shares sum to 1.
	totalReward := DerefInt(initiative.Budget)
	completerReward := math.LegacyNewDecFromInt(totalReward).Mul(params.CompleterShare).TruncateInt()
	treasuryShare := math.LegacyNewDecFromInt(totalReward).Mul(params.TreasuryShare).TruncateInt()
	totalInitiativeMint := completerReward.Add(treasuryShare)

	// Check the per-season minting cap against EVERY DREAM this completion will
	// create, not just the completer and treasury shares. The staker completion
	// bonus and the reviewers' fee are minted further down this same function
	// and counted afterwards, so a gate that ignored them would admit a
	// completion and then mint past the cap it had just checked — the overrun
	// bounded only by the size of the last initiative through the door.
	//
	// Both projections are upper bounds. The bonus pays nothing when no
	// external staker holds conviction, and both truncate per recipient, so the
	// gate is conservative: it can refuse a completion that would have fitted,
	// never admit one that does not. For a cap, erring that way is correct.
	projectedBonus := math.ZeroInt()
	if hasStakes, sErr := k.InitiativeHasStakes(ctx, initiativeID); sErr == nil && hasStakes {
		projectedBonus = k.InitiativeCompletionBonusPool(ctx, totalReward)
	}
	// Only project a review fee if the round actually has verdicts to pay for.
	// PayReviewFees returns early on an empty round, so charging the gate for a
	// review that never happened would refuse completions for a mint that is
	// not coming.
	projectedReviewFees := math.ZeroInt()
	if reviews, rErr := k.GetInitiativeReviews(ctx, initiative.Id, initiative.ReviewRound); rErr == nil && len(reviews) > 0 {
		projectedReviewFees = k.ReviewFeePool(ctx, params, initiative)
	}

	// The member- and tag-stake revenue shares this completion accrues are
	// minted later, when those stakers settle — but they are DREAM created by
	// initiative completion all the same, and the cap's own contract is "total
	// DREAM minted via initiative completion". Both are upper bounds: the
	// pools may be empty or missing, in which case less accrues and less is
	// eventually minted. The actually-accrued figures are tracked at the
	// accumulate calls below so the counter converges on reality.
	projectedMemberShare := params.MemberStakeRevenueShare.MulInt(completerReward).TruncateInt()
	projectedTagShare := math.ZeroInt()
	if len(initiative.Tags) > 0 {
		projectedTagShare = params.TagStakeRevenueShare.MulInt(completerReward).TruncateInt()
	}

	projectedTotalMint := totalInitiativeMint.Add(projectedBonus).Add(projectedReviewFees).
		Add(projectedMemberShare).Add(projectedTagShare)

	seasonRewardsMinted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get season initiative rewards: %w", err)
	}
	if seasonRewardsMinted.Add(projectedTotalMint).GT(params.MaxInitiativeRewardsPerSeason) {
		return fmt.Errorf("completing this initiative would mint up to %s DREAM (completer+treasury %s, staker bonus %s, review fees %s, member/tag stake shares %s), exceeding season cap of %s (already minted %s): %w",
			projectedTotalMint.String(), totalInitiativeMint.String(), projectedBonus.String(),
			projectedReviewFees.String(), projectedMemberShare.Add(projectedTagShare).String(),
			params.MaxInitiativeRewardsPerSeason.String(),
			seasonRewardsMinted.String(), types.ErrInitiativeRewardCapReached)
	}

	// Mint DREAM to assignee (completer)
	assigneeAddr, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return fmt.Errorf("invalid assignee address: %w", err)
	}
	if err := k.MintDREAM(ctx, assigneeAddr, completerReward); err != nil {
		return fmt.Errorf("failed to mint DREAM for completer: %w", err)
	}

	// Mint the TreasuryShare into the module treasury ledger. Uses
	// MintToTreasury so the per-epoch mint cap is enforced and SeasonMinted
	// + SeasonTreasuryInflow are both advanced.
	if treasuryShare.IsPositive() {
		if err := k.MintToTreasury(ctx, treasuryShare); err != nil {
			return fmt.Errorf("failed to mint treasury share: %w", err)
		}
	}

	// Track initiative reward minting against the per-season cap (both halves).
	if err := k.TrackInitiativeRewardMint(ctx, totalInitiativeMint); err != nil {
		return fmt.Errorf("failed to track initiative reward mint: %w", err)
	}

	// Distribute staking rewards to stakers based on time-weighted APY
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("failed to get stakes: %w", err)
	}

	// Get SDK context for event emission
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Distribute the conviction-weighted completion bonus BEFORE the payout
	// loop below deletes the stake records. The bonus is weighted by each
	// stake's time-weighted conviction, which is derived from created_at, so
	// running it afterwards left it with an empty stake set and it silently
	// paid nothing to anyone.
	if len(stakes) > 0 {
		if err := k.DistributeInitiativeCompletionBonus(ctx, initiativeID, totalReward); err != nil {
			return fmt.Errorf("failed to distribute initiative completion bonus: %w", err)
		}
	}

	// Pay the reviewers who judged the round that resolved this initiative.
	// Paid per verdict filed, never per approval — and paid on this path and
	// the close path alike, so the fee never depends on the outcome.
	if _, err := k.PayReviewFees(ctx, initiative); err != nil {
		return fmt.Errorf("failed to pay review fees: %w", err)
	}
	// Any escrowed bounty settles on the same terms and the same path: split
	// across the verdicts filed, refunded to funders if there were none.
	if _, err := k.PayReviewBounty(ctx, initiative); err != nil {
		return fmt.Errorf("failed to settle review bounty: %w", err)
	}
	// Completion is gated on the challenge window having elapsed with no active
	// challenge, so these verdicts are no longer contestable and the bond
	// committed against them is free.
	if err := k.SettleReviewBonds(ctx, initiativeID); err != nil {
		return fmt.Errorf("failed to settle review bonds: %w", err)
	}

	// Settle and release every stake.
	for _, stake := range stakes {
		stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
		if err != nil {
			continue
		}

		// Harvest whatever the stake accrued from the seasonal pool and zero
		// its debt — the record is about to be deleted.
		settledStake, settlement, err := k.settleStake(ctx, stake, math.ZeroInt(), false)
		if err != nil {
			return fmt.Errorf("failed to settle stake %d for %s: %w", stake.Id, stake.Staker, err)
		}

		// Unlock staked DREAM
		if err := k.UnlockDREAM(ctx, stakerAddr, stake.Amount); err != nil {
			return fmt.Errorf("failed to unlock DREAM for staker %s: %w", stake.Staker, err)
		}

		// Shrink the seasonal denominator by the stake leaving it. Missing this
		// would ratchet total_staked upward with every completed initiative and
		// under-pay the remaining stakers forever.
		if err := k.updateStakePoolTotals(ctx, settledStake, stake.Amount.Neg()); err != nil {
			return fmt.Errorf("failed to update stake pool totals for stake %d: %w", stake.Id, err)
		}

		// Remove stake from target index
		_ = k.RemoveStakeFromTargetIndex(ctx, stake)

		// Remove stake
		if err := k.Stake.Remove(ctx, stake.Id); err != nil {
			return fmt.Errorf("failed to remove stake: %w", err)
		}

		// Emit event for stake completion
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"stake_completed",
				sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
				sdk.NewAttribute("staker", stake.Staker),
				sdk.NewAttribute("amount", stake.Amount.String()),
				sdk.NewAttribute("reward", settlement.Minted.String()),
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			),
		)
	}

	// Grant reputation to completer
	member, err := k.GetMember(ctx, assigneeAddr)
	if err != nil {
		return fmt.Errorf("failed to get member: %w", err)
	}

	// Calculate reputation grant based on tier
	var tierConfig types.TierConfig
	switch initiative.Tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
	}

	// Grant reputation split evenly across tags (subject to per-epoch cap).
	// Total reputation = budget / 10, divided by tag count.
	// E.g., 2000 DREAM budget with 3 tags → 66.6 rep per tag instead of 200 per tag.
	tagCount := int64(len(initiative.Tags))
	if tagCount == 0 {
		tagCount = 1
	}
	for _, tag := range initiative.Tags {
		currentRep := math.LegacyZeroDec()
		if repStr, ok := member.ReputationScores[tag]; ok {
			currentRep, _ = math.LegacyNewDecFromStr(repStr)
		}

		// Reputation grant = min(budget / 10 / tagCount, tier cap - current rep)
		repGrant := math.LegacyNewDecFromInt(DerefInt(initiative.Budget)).QuoInt64(10).QuoInt64(tagCount)
		maxGrant := tierConfig.ReputationCap.Sub(currentRep)
		if repGrant.GT(maxGrant) {
			repGrant = maxGrant
		}

		if repGrant.GT(math.LegacyZeroDec()) {
			// Use capped grant to prevent reputation grinding
			if _, err := k.GrantReputationCapped(ctx, &member, tag, repGrant); err != nil {
				return fmt.Errorf("failed to grant reputation for tag %s: %w", tag, err)
			}
		}
	}

	// Increment completed initiatives count for potential future use (O(1) lookup)
	member.CompletedInitiativesCount++

	// Update member
	if err := k.Member.Set(ctx, assigneeAddr.String(), member); err != nil {
		return fmt.Errorf("failed to update member: %w", err)
	}

	// Check for trust level upgrade after reputation change (lazy evaluation)
	// This is a trigger point - we only check when reputation actually changes
	_ = k.UpdateTrustLevel(ctx, assigneeAddr)

	// Distribute revenue share to member stakers
	// Members who stake on the assignee receive a portion of the initiative earnings.
	// The accrued figures are counted against the per-season initiative reward
	// cap below — this DREAM is minted when the stakers settle, but it is
	// created by this completion, and the cap covers everything a completion
	// creates. Accrual (not the notional share) is tracked, so pools with
	// nothing staked contribute nothing.
	memberShareAccrued := math.ZeroInt()
	if accrued, mErr := k.AccumulateMemberStakeRevenue(ctx, assigneeAddr, completerReward); mErr != nil {
		// Log but don't fail - stake pools might not exist
		sdkCtx.Logger().Debug("failed to accumulate member stake revenue", "error", mErr, "member", assigneeAddr)
	} else {
		memberShareAccrued = accrued
	}

	// Distribute revenue share to tag stakers
	// Members who stake on matching tags receive a portion of the initiative earnings
	tagShareAccrued := math.ZeroInt()
	if len(initiative.Tags) > 0 {
		if accrued, tErr := k.AccumulateTagStakeRevenue(ctx, initiative.Tags, completerReward); tErr != nil {
			sdkCtx.Logger().Debug("failed to accumulate tag stake revenue", "error", tErr, "tags", initiative.Tags)
		} else {
			tagShareAccrued = accrued
		}
	}
	if stakeShareTotal := memberShareAccrued.Add(tagShareAccrued); stakeShareTotal.IsPositive() {
		if err := k.TrackInitiativeRewardMint(ctx, stakeShareTotal); err != nil {
			return fmt.Errorf("failed to track member/tag stake revenue share mint: %w", err)
		}
	}

	// Mark budget as spent in project (skip for permissionless — no pre-allocated budget)
	completionProject, projErr := k.GetProject(ctx, initiative.ProjectId)
	if projErr == nil && !completionProject.Permissionless {
		if err := k.SpendBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
			return fmt.Errorf("failed to mark budget as spent: %w", err)
		}
	}

	// Return any self-assign bond to the assignee
	if err := k.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
		return err
	}

	// Update initiative
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
	initiative.CompletedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_completed",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("assignee", initiative.Assignee),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", initiative.ProjectId)),
			sdk.NewAttribute("budget", initiative.Budget.String()),
			sdk.NewAttribute("completer_reward", completerReward.String()),
		),
	)

	return nil
}

// GetMember retrieves a member by address with lazy decay applied.
// This is the canonical way to get a member - it ensures decay is always current.
// Note: This applies and persists decay. For read-only access without persistence,
// use Member.Get directly and call ApplyPendingDecay without saving.
func (k Keeper) GetMember(ctx context.Context, address sdk.AccAddress) (types.Member, error) {
	member, err := k.Member.Get(ctx, address.String())
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Member{}, fmt.Errorf("member not found: %s", address.String())
		}
		return types.Member{}, err
	}

	// Both decay passes are no-ops once the member is current for this epoch —
	// they share the LastDecayEpoch guard. Checking it here lets GetMember skip
	// the write as well, not just the arithmetic. That matters because GetMember
	// is called from hot paths (notably the per-block conviction sweep, once per
	// stake), and an unconditional Set re-hashes the member's IAVL node on every
	// commit, making state growth and commit cost scale with stake count rather
	// than with real activity.
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return types.Member{}, err
	}
	if member.LastDecayEpoch >= currentEpoch {
		return member, nil
	}

	// Apply lazy reputation decay before DREAM decay (both use LastDecayEpoch).
	// Reputation decay must run first since it reads the same epoch field
	// but doesn't update it — ApplyPendingDecay handles the epoch advancement.
	if err := k.ApplyReputationDecay(ctx, &member); err != nil {
		return types.Member{}, err
	}

	// Apply lazy DREAM balance decay - this ensures balances are always accurate
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return types.Member{}, err
	}

	// Persist the updated decay state
	if err := k.Member.Set(ctx, address.String(), member); err != nil {
		return types.Member{}, err
	}

	return member, nil
}

// settleInitiativeStakes harvests every stake on an initiative against the
// seasonal accumulator and rebases its debt, leaving the principal locked and
// the record in place. It is the initiative-side twin of settleProjectStakes.
//
// Called at both terminal transitions that do NOT delete their stakes —
// CloseInitiative and the challenge-REJECTED path — and always BEFORE the
// status flip, because stakeAccruing stops paying the moment the status is
// terminal and settling afterwards would harvest nothing. CompleteInitiative
// does not use this: it settles and deletes each stake in its own payout loop.
//
// Rewards accrued while the work was live are paid regardless of how the
// initiative ended. The outcome does not retroactively unearn them — that is
// what slashing is for — and this matches CancelProject, which pays out on the
// cancel path too.
//
// A per-stake settle failure (the per-epoch mint cap, most plausibly) is logged
// and the pending forfeited rather than blocking the transition: an initiative
// that cannot be retired is the failure mode that stranded devnet initiative #1
// in IN_REVIEW for ~6,000 blocks.
func (k Keeper) settleInitiativeStakes(ctx context.Context, initiativeID uint64) error {
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("failed to load stakes of initiative %d for settlement: %w", initiativeID, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, stake := range stakes {
		settled, settlement, err := k.settleStake(ctx, stake, stake.Amount, false)
		if err != nil {
			sdkCtx.Logger().Error("failed to settle initiative stake at terminal transition; pending forfeited",
				"initiative_id", initiativeID, "stake_id", stake.Id, "staker", stake.Staker, "error", err)
			continue
		}
		if err := k.Stake.Set(ctx, stake.Id, settled); err != nil {
			return fmt.Errorf("failed to persist settled stake %d: %w", stake.Id, err)
		}
		if settlement.Minted.IsPositive() {
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"initiative_stake_settled",
					sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
					sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
					sdk.NewAttribute("staker", stake.Staker),
					sdk.NewAttribute("rewards", settlement.Minted.String()),
				),
			)
		}
	}
	return nil
}

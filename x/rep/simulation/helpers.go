package simulation

import (
	"errors"
	"math/rand"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// findMember returns a random member from the state, or nil if none exist
func findMember(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Member, error) {
	var members []types.Member
	err := k.Member.Walk(ctx, nil, func(key string, member types.Member) (bool, error) {
		members = append(members, member)
		return false, nil
	})
	if err != nil || len(members) == 0 {
		return nil, err
	}
	return &members[r.Intn(len(members))], nil
}

// findMemberWithDream returns a random member with sufficient DREAM balance
func findMemberWithDream(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, minAmount math.Int) (*types.Member, error) {
	var members []types.Member
	err := k.Member.Walk(ctx, nil, func(key string, member types.Member) (bool, error) {
		if member.DreamBalance != nil && member.DreamBalance.GTE(minAmount) {
			members = append(members, member)
		}
		return false, nil
	})
	if err != nil || len(members) == 0 {
		return nil, err
	}
	return &members[r.Intn(len(members))], nil
}

// findInvitation returns a random pending invitation, or nil if none exist
func findInvitation(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.InvitationStatus) (*types.Invitation, uint64, error) {
	var invitations []struct {
		id         uint64
		invitation types.Invitation
	}
	err := k.Invitation.Walk(ctx, nil, func(id uint64, invitation types.Invitation) (bool, error) {
		if invitation.Status == status {
			invitations = append(invitations, struct {
				id         uint64
				invitation types.Invitation
			}{id, invitation})
		}
		return false, nil
	})
	if err != nil || len(invitations) == 0 {
		return nil, 0, err
	}
	selected := invitations[r.Intn(len(invitations))]
	return &selected.invitation, selected.id, nil
}

// findProject returns a random project with the given status
func findProject(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.ProjectStatus) (*types.Project, uint64, error) {
	var projects []struct {
		id      uint64
		project types.Project
	}
	err := k.Project.Walk(ctx, nil, func(id uint64, project types.Project) (bool, error) {
		if project.Status == status {
			projects = append(projects, struct {
				id      uint64
				project types.Project
			}{id, project})
		}
		return false, nil
	})
	if err != nil || len(projects) == 0 {
		return nil, 0, err
	}
	selected := projects[r.Intn(len(projects))]
	return &selected.project, selected.id, nil
}

// findInitiative returns a random initiative with the given status
func findInitiative(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.InitiativeStatus) (*types.Initiative, uint64, error) {
	var initiatives []struct {
		id         uint64
		initiative types.Initiative
	}
	err := k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.Status == status {
			initiatives = append(initiatives, struct {
				id         uint64
				initiative types.Initiative
			}{id, initiative})
		}
		return false, nil
	})
	if err != nil || len(initiatives) == 0 {
		return nil, 0, err
	}
	selected := initiatives[r.Intn(len(initiatives))]
	return &selected.initiative, selected.id, nil
}

// findInitiativeByAssignee returns a random initiative assigned to the given member
func findInitiativeByAssignee(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, assignee string, status types.InitiativeStatus) (*types.Initiative, uint64, error) {
	var initiatives []struct {
		id         uint64
		initiative types.Initiative
	}
	err := k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.Assignee == assignee && initiative.Status == status {
			initiatives = append(initiatives, struct {
				id         uint64
				initiative types.Initiative
			}{id, initiative})
		}
		return false, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(initiatives) == 0 {
		return nil, 0, errors.New("initiative not found")
	}
	selected := initiatives[r.Intn(len(initiatives))]
	return &selected.initiative, selected.id, nil
}

// findStake returns a random stake
func findStake(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Stake, uint64, error) {
	var stakes []struct {
		id    uint64
		stake types.Stake
	}
	err := k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		stakes = append(stakes, struct {
			id    uint64
			stake types.Stake
		}{id, stake})
		return false, nil
	})
	if err != nil || len(stakes) == 0 {
		return nil, 0, err
	}
	selected := stakes[r.Intn(len(stakes))]
	return &selected.stake, selected.id, nil
}

// findStakeByStaker returns a random stake by the given staker
func findStakeByStaker(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, staker string) (*types.Stake, uint64, error) {
	var stakes []struct {
		id    uint64
		stake types.Stake
	}
	err := k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		if stake.Staker == staker {
			stakes = append(stakes, struct {
				id    uint64
				stake types.Stake
			}{id, stake})
		}
		return false, nil
	})
	if err != nil || len(stakes) == 0 {
		return nil, 0, err
	}
	selected := stakes[r.Intn(len(stakes))]
	return &selected.stake, selected.id, nil
}

// findChallenge returns a random challenge with the given status
func findChallenge(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.ChallengeStatus) (*types.Challenge, uint64, error) {
	var challenges []struct {
		id        uint64
		challenge types.Challenge
	}
	err := k.Challenge.Walk(ctx, nil, func(id uint64, challenge types.Challenge) (bool, error) {
		if challenge.Status == status {
			challenges = append(challenges, struct {
				id        uint64
				challenge types.Challenge
			}{id, challenge})
		}
		return false, nil
	})
	if err != nil || len(challenges) == 0 {
		return nil, 0, err
	}
	selected := challenges[r.Intn(len(challenges))]
	return &selected.challenge, selected.id, nil
}

// findInterim returns a random interim with the given status
func findInterim(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.InterimStatus) (*types.Interim, uint64, error) {
	var interims []struct {
		id      uint64
		interim types.Interim
	}
	err := k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		if interim.Status == status {
			interims = append(interims, struct {
				id      uint64
				interim types.Interim
			}{id, interim})
		}
		return false, nil
	})
	if err != nil || len(interims) == 0 {
		return nil, 0, err
	}
	selected := interims[r.Intn(len(interims))]
	return &selected.interim, selected.id, nil
}

// findInterimByAssignee returns a random interim assigned to the given member
func findInterimByAssignee(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, assignee string, status types.InterimStatus) (*types.Interim, uint64, error) {
	var interims []struct {
		id      uint64
		interim types.Interim
	}
	err := k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		if interim.Status == status {
			for _, a := range interim.Assignees {
				if a == assignee {
					interims = append(interims, struct {
						id      uint64
						interim types.Interim
					}{id, interim})
					break
				}
			}
		}
		return false, nil
	})
	if err != nil || len(interims) == 0 {
		return nil, 0, err
	}
	selected := interims[r.Intn(len(interims))]
	return &selected.interim, selected.id, nil
}

// randomTag returns a random tag from common tags
func randomTag(r *rand.Rand) string {
	tags := []string{"backend", "frontend", "design", "devops", "documentation", "testing"}
	return tags[r.Intn(len(tags))]
}

// randomTags returns a random list of tags (1-3 tags)
func randomTags(r *rand.Rand) []string {
	numTags := r.Intn(3) + 1
	tags := make([]string, numTags)
	for i := 0; i < numTags; i++ {
		tags[i] = randomTag(r)
	}
	return tags
}

// randomProjectCategory returns a random project category
func randomProjectCategory(r *rand.Rand) types.ProjectCategory {
	categories := []types.ProjectCategory{
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM,
		types.ProjectCategory_PROJECT_CATEGORY_RESEARCH,
		types.ProjectCategory_PROJECT_CATEGORY_CREATIVE,
	}
	return categories[r.Intn(len(categories))]
}

// randomInitiativeTier returns a random initiative tier
func randomInitiativeTier(r *rand.Rand) types.InitiativeTier {
	tiers := []types.InitiativeTier{
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeTier_INITIATIVE_TIER_EXPERT,
		types.InitiativeTier_INITIATIVE_TIER_EPIC,
	}
	return tiers[r.Intn(len(tiers))]
}

// randomInitiativeCategory returns a random initiative category
func randomInitiativeCategory(r *rand.Rand) types.InitiativeCategory {
	categories := []types.InitiativeCategory{
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_BUGFIX,
		types.InitiativeCategory_INITIATIVE_CATEGORY_REFACTOR,
		types.InitiativeCategory_INITIATIVE_CATEGORY_DOCUMENTATION,
		types.InitiativeCategory_INITIATIVE_CATEGORY_TESTING,
	}
	return categories[r.Intn(len(categories))]
}

// randomInterimType returns a random interim type
func randomInterimType(r *rand.Rand) types.InterimType {
	interimTypes := []types.InterimType{
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
		types.InterimType_INTERIM_TYPE_MENTORSHIP,
		types.InterimType_INTERIM_TYPE_AUDIT,
	}
	return interimTypes[r.Intn(len(interimTypes))]
}

// randomCouncil returns a random council name
func randomCouncil(r *rand.Rand) string {
	councils := []string{"technical", "ecosystem", "commons"}
	return councils[r.Intn(len(councils))]
}

// randomCommittee returns a random committee name
func randomCommittee(r *rand.Rand) string {
	committees := []string{"operations", "hr", "finance"}
	return committees[r.Intn(len(committees))]
}

// ensureMemberExists creates a member if it doesn't exist
func ensureMemberExists(ctx sdk.Context, k keeper.Keeper, addr sdk.AccAddress) error {
	_, err := k.Member.Get(ctx, addr.String())
	if err != nil {
		// Member doesn't exist, create it
		member := types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.NewInt(1000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
			ReputationScores: make(map[string]string),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
		}
		return k.Member.Set(ctx, addr.String(), member)
	}
	return nil
}

// PtrInt returns a pointer to a math.Int
func PtrInt(i math.Int) *math.Int {
	return &i
}

// getAccountFromMember converts a member address string to a simtypes.Account
func getAccountFromMember(member *types.Member, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == member.Address {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// getOrCreateMember returns an existing member or creates a new one
func getOrCreateMember(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) (*types.Member, simtypes.Account, error) {
	// Try to find existing member
	member, err := findMember(r, ctx, k)
	if err == nil && member != nil {
		acc, found := getAccountFromMember(member, accs)
		if found {
			return member, acc, nil
		}
	}

	// Create new member
	simAccount, _ := simtypes.RandomAcc(r, accs)
	if err := ensureMemberExists(ctx, k, simAccount.Address); err != nil {
		return nil, simtypes.Account{}, err
	}

	newMember := &types.Member{
		Address:           simAccount.Address.String(),
		DreamBalance:      PtrInt(math.NewInt(int64(r.Intn(9000) + 1000))), // 1000-10000 DREAM
		StakedDream:       PtrInt(math.ZeroInt()),
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		InvitationCredits: uint32(r.Intn(5) + 2), // 2-6 invitation credits
		ReputationScores:  make(map[string]string),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
	}

	if err := k.Member.Set(ctx, simAccount.Address.String(), *newMember); err != nil {
		return nil, simtypes.Account{}, err
	}

	return newMember, simAccount, nil
}

// getOrCreateMemberWithDream returns a member with sufficient DREAM or creates one
func getOrCreateMemberWithDream(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account, minAmount math.Int) (*types.Member, simtypes.Account, error) {
	// Try to find existing member with enough DREAM
	member, err := findMemberWithDream(r, ctx, k, minAmount)
	if err == nil && member != nil {
		acc, found := getAccountFromMember(member, accs)
		if found {
			return member, acc, nil
		}
	}

	// Create new member with sufficient DREAM
	simAccount, _ := simtypes.RandomAcc(r, accs)
	dreamAmount := minAmount.MulRaw(int64(r.Intn(10) + 2)) // 2x-12x minimum

	newMember := types.Member{
		Address:           simAccount.Address.String(),
		DreamBalance:      PtrInt(dreamAmount),
		StakedDream:       PtrInt(math.ZeroInt()),
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		InvitationCredits: uint32(r.Intn(5) + 2), // 2-6 invitation credits
		ReputationScores:  make(map[string]string),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
	}

	if err := k.Member.Set(ctx, simAccount.Address.String(), newMember); err != nil {
		return nil, simtypes.Account{}, err
	}

	return &newMember, simAccount, nil
}

// getOrCreateMemberWithReputation returns a member with sufficient reputation for a tier or creates one
func getOrCreateMemberWithReputation(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account, tier types.InitiativeTier, tags []string) (*types.Member, simtypes.Account, error) {
	// Get params to determine reputation requirement
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, simtypes.Account{}, err
	}

	var tierConfig types.TierConfig
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		tierConfig = params.ApprenticeTier
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		tierConfig = params.StandardTier
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		tierConfig = params.ExpertTier
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		tierConfig = params.EpicTier
	}

	// Try to find an existing member with sufficient reputation
	var members []types.Member
	k.Member.Walk(ctx, nil, func(key string, member types.Member) (bool, error) {
		// Check if member has sufficient reputation for the tags
		totalRep := math.LegacyZeroDec()
		for _, tag := range tags {
			if repStr, ok := member.ReputationScores[tag]; ok {
				rep, err := math.LegacyNewDecFromStr(repStr)
				if err == nil {
					totalRep = totalRep.Add(rep)
				}
			}
		}
		avgRep := math.LegacyZeroDec()
		if len(tags) > 0 {
			avgRep = totalRep.QuoInt64(int64(len(tags)))
		}

		if avgRep.GTE(tierConfig.MinReputation) {
			members = append(members, member)
		}
		return false, nil
	})

	if len(members) > 0 {
		member := members[r.Intn(len(members))]
		acc, found := getAccountFromMember(&member, accs)
		if found {
			return &member, acc, nil
		}
	}

	// Create new member with sufficient reputation
	simAccount, _ := simtypes.RandomAcc(r, accs)

	// Set reputation slightly above minimum for each tag
	repScores := make(map[string]string)
	minRep := tierConfig.MinReputation
	if minRep.IsZero() {
		minRep = math.LegacyNewDec(10) // Default to 10 for apprentice tier
	}
	// Add 10-50% buffer above minimum
	repValue := minRep.Mul(math.LegacyNewDecWithPrec(int64(110+r.Intn(40)), 2))

	for _, tag := range tags {
		repScores[tag] = repValue.String()
	}

	newMember := types.Member{
		Address:           simAccount.Address.String(),
		DreamBalance:      PtrInt(math.NewInt(int64(r.Intn(9000) + 1000))), // 1000-10000 DREAM
		StakedDream:       PtrInt(math.ZeroInt()),
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		InvitationCredits: uint32(r.Intn(5) + 2), // 2-6 invitation credits
		ReputationScores:  repScores,
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
	}

	if err := k.Member.Set(ctx, simAccount.Address.String(), newMember); err != nil {
		return nil, simtypes.Account{}, err
	}

	return &newMember, simAccount, nil
}

// createInvitation creates a new invitation and locks the inviter's DREAM
func createInvitation(ctx sdk.Context, k keeper.Keeper, r *rand.Rand, inviter *types.Member, inviteeAddr string) (uint64, error) {
	invitationID, err := k.InvitationSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	stakeAmount := math.NewInt(int64(r.Intn(400) + 100)) // 100-500 DREAM

	// Ensure inviter has enough DREAM to stake
	if inviter.DreamBalance == nil || inviter.DreamBalance.LT(stakeAmount) {
		stakeAmount = math.NewInt(50) // Use minimum amount
		if inviter.DreamBalance == nil || inviter.DreamBalance.LT(stakeAmount) {
			// Can't create invitation without enough balance
			return 0, errors.New("insufficient DREAM balance for invitation stake")
		}
	}

	// Lock the staked DREAM by updating the inviter's member record
	if inviter.StakedDream == nil {
		inviter.StakedDream = PtrInt(stakeAmount)
	} else {
		*inviter.StakedDream = inviter.StakedDream.Add(stakeAmount)
	}
	if err := k.Member.Set(ctx, inviter.Address, *inviter); err != nil {
		return 0, err
	}

	invitation := types.Invitation{
		Id:             invitationID,
		Inviter:        inviter.Address,
		InviteeAddress: inviteeAddr,
		Status:         types.InvitationStatus_INVITATION_STATUS_PENDING,
		StakedDream:    &stakeAmount,
		VouchedTags:    randomTags(r),
	}

	return invitationID, k.Invitation.Set(ctx, invitationID, invitation)
}

// getOrCreateProject returns an existing active project or creates one
func getOrCreateProject(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator *types.Member) (uint64, error) {
	// Try to find existing active project
	project, projectID, err := findProject(r, ctx, k, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
	if err == nil && project != nil {
		return projectID, nil
	}

	// Create new project
	projectID, err = k.ProjectSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	budgetDream := math.NewInt(int64(r.Intn(90000) + 10000)) // 10k-100k DREAM

	newProject := types.Project{
		Id:              projectID,
		Creator:         creator.Address,
		Name:            randomName(r, "Project"),
		Description:     "Simulation generated project",
		Tags:            randomTags(r),
		Category:        randomProjectCategory(r),
		Council:         randomCouncil(r),
		Status:          types.ProjectStatus_PROJECT_STATUS_ACTIVE,
		ApprovedBudget:  &budgetDream,
		AllocatedBudget: PtrInt(math.ZeroInt()),
		SpentBudget:     PtrInt(math.ZeroInt()),
	}

	return projectID, k.Project.Set(ctx, projectID, newProject)
}

// getOrCreateInitiative returns an existing initiative or creates one
func getOrCreateInitiative(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator *types.Member, status types.InitiativeStatus) (uint64, error) {
	// Try to find existing initiative with the desired status
	initiative, initID, err := findInitiative(r, ctx, k, status)
	if err == nil && initiative != nil {
		return initID, nil
	}

	// Need to create initiative, first ensure we have a project
	projectID, err := getOrCreateProject(r, ctx, k, creator)
	if err != nil {
		return 0, err
	}

	// Create new initiative
	initID, err = k.InitiativeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	tier := randomInitiativeTier(r)
	budget := calculateBudgetByTier(r, tier)

	newInitiative := types.Initiative{
		Id:          initID,
		ProjectId:   projectID,
		Title:       randomName(r, "Initiative"),
		Description: "Simulation generated initiative",
		Tags:        randomTags(r),
		Tier:        tier,
		Category:    randomInitiativeCategory(r),
		Status:      status,
		Budget:      &budget,
	}

	// Set assignee if status requires it
	if status == types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED ||
		status == types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED {
		newInitiative.Assignee = creator.Address
	}

	// Bypass CreateInitiative, so mirror its budget allocation on the project
	// (keeps AllocatedBudget consistent with outstanding initiatives; non-permissionless only).
	if project, perr := k.GetProject(ctx, projectID); perr == nil && !project.Permissionless {
		project.AllocatedBudget = PtrInt(keeper.DerefInt(project.AllocatedBudget).Add(budget))
		_ = k.Project.Set(ctx, projectID, project)
	}

	return initID, k.Initiative.Set(ctx, initID, newInitiative)
}

// randomName generates a random name with prefix
func randomName(r *rand.Rand, prefix string) string {
	return simtypes.RandStringOfLength(r, 8) + "-" + prefix
}

// calculateBudgetByTier returns appropriate budget for initiative tier
func calculateBudgetByTier(r *rand.Rand, tier types.InitiativeTier) math.Int {
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		return math.NewInt(int64(r.Intn(400) + 100)) // 100-500
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		return math.NewInt(int64(r.Intn(900) + 600)) // 600-1500
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		return math.NewInt(int64(r.Intn(2000) + 1500)) // 1500-3500
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		return math.NewInt(int64(r.Intn(4000) + 3500)) // 3500-7500
	default:
		return math.NewInt(1000)
	}
}

// createStake creates a stake on an initiative
func createStake(ctx sdk.Context, k keeper.Keeper, r *rand.Rand, staker *types.Member, targetID uint64) (uint64, error) {
	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	// Stake 10-50% of available DREAM
	maxStake := staker.DreamBalance.QuoRaw(2)
	minStake := math.NewInt(50)
	if maxStake.LT(minStake) {
		maxStake = minStake
	}

	// Calculate stake amount with safe random range
	var stakeAmount math.Int
	rangeVal := maxStake.Sub(minStake).Int64()
	if rangeVal > 0 {
		stakeAmount = math.NewInt(int64(r.Intn(int(rangeVal))) + minStake.Int64())
	} else {
		stakeAmount = minStake
	}

	stake := types.Stake{
		Id:            stakeID,
		Staker:        staker.Address,
		TargetType:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId:      targetID,
		Amount:        stakeAmount,
		CreatedAt:     ctx.BlockTime().Unix(),
		LastClaimedAt: 0,
		RewardDebt:    math.ZeroInt(),
	}

	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return 0, err
	}

	// Update member's staked dream and balance
	staker.DreamBalance = PtrInt(staker.DreamBalance.Sub(stakeAmount))
	staker.StakedDream = PtrInt(staker.StakedDream.Add(stakeAmount))
	if err := k.Member.Set(ctx, staker.Address, *staker); err != nil {
		return 0, err
	}

	return stakeID, nil
}

// createChallenge creates a challenge on an initiative
func createChallenge(ctx sdk.Context, k keeper.Keeper, r *rand.Rand, challenger *types.Member, initiativeID uint64) (uint64, error) {
	challengeID, err := k.ChallengeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	stakeAmount := math.NewInt(int64(r.Intn(400) + 100)) // 100-500 DREAM

	challenge := types.Challenge{
		Id:           challengeID,
		Challenger:   challenger.Address,
		InitiativeId: initiativeID,
		Reason:       "Simulation challenge",
		Evidence:     []string{"Simulated evidence"},
		StakedDream:  &stakeAmount,
		Status:       types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
	}

	return challengeID, k.Challenge.Set(ctx, challengeID, challenge)
}

// createInterim creates an interim work item
func createInterim(ctx sdk.Context, k keeper.Keeper, r *rand.Rand, creator *types.Member) (uint64, error) {
	interimID, err := k.InterimSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	budget := math.NewInt(int64(r.Intn(900) + 100)) // 100-1000 DREAM

	interim := types.Interim{
		Id:         interimID,
		Type:       randomInterimType(r),
		Assignees:  []string{creator.Address},
		Committee:  randomCommittee(r),
		Status:     types.InterimStatus_INTERIM_STATUS_PENDING,
		Budget:     &budget,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
	}

	return interimID, k.Interim.Set(ctx, interimID, interim)
}


// getOrCreateSimTagBudget returns an existing tag budget or creates one for the
// simulation. It does not enforce group-membership or SPARK escrow checks —
// those are exercised in unit + integration tests.
func getOrCreateSimTagBudget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, groupAccount string) (uint64, error) {
	var existing uint64
	_ = k.TagBudget.Walk(ctx, nil, func(id uint64, budget types.TagBudget) (bool, error) {
		if budget.Active {
			existing = id
			return true, nil
		}
		return false, nil
	})
	if existing != 0 {
		return existing, nil
	}

	budgetID, err := k.TagBudgetSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	newBudget := types.TagBudget{
		Id:           budgetID,
		GroupAccount: groupAccount,
		Tag:          randomTagBudgetTag(r),
		PoolBalance:  "100000",
		MembersOnly:  false,
		CreatedAt:    ctx.BlockTime().Unix(),
		Active:       true,
	}
	return budgetID, k.TagBudget.Set(ctx, budgetID, newBudget)
}

// randomTagBudgetTag returns a stable fictional tag name. The tag registry
// lives in x/rep but simulation does not register tags here; these names just
// need to be valid strings for the budget record.
func randomTagBudgetTag(r *rand.Rand) string {
	tags := []string{"golang", "rust", "python", "design", "docs", "frontend", "backend"}
	return tags[r.Intn(len(tags))]
}


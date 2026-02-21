package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	module "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

func TestCreateChallenge(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create an initiative to challenge
	projectID, err := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr1")),
		"Test Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, creator, math.NewInt(1000))
	initID, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Test Initiative",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	// Assign initiative to creator (so they can submit)
	err = k.AssignInitiativeToMember(ctx, initID, creator)
	require.NoError(t, err)

	// Submit initiative
	err = k.SubmitInitiativeWork(ctx, initID, creator, "Deliverable URI")
	require.NoError(t, err)

	// Test Case 1: Normal Challenge
	challenger := sdk.AccAddress([]byte("challenger"))
	stakedAmount := math.NewInt(50) // Min stake
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	chalID, err := k.CreateChallenge(
		ctx,
		challenger,
		initID,
		"Bad work",
		[]string{"evidence1"},
		stakedAmount,
		false,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)

	// Verify challenge state
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, challenger.String(), challenge.Challenger)
	require.False(t, challenge.IsAnonymous)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)

	// Verify initiative state
	init, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, init.Status)

	// Test Case 2: Anonymous Challenge (uses SPARK escrow, not DREAM)
	nullifier := []byte("nullifier1")
	proof := []byte("proof1")

	// Track bank keeper calls for SPARK escrow
	var escrowedCoins sdk.Coins
	var escrowSender sdk.AccAddress
	fixture.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		escrowedCoins = amt
		escrowSender = senderAddr
		require.Equal(t, "rep", recipientModule)
		return nil
	}

	// Re-submit another initiative or use same project
	initID2, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Test Initiative 2",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	err = k.AssignInitiativeToMember(ctx, initID2, creator)
	require.NoError(t, err)

	err = k.SubmitInitiativeWork(ctx, initID2, creator, "Deliverable URI")
	require.NoError(t, err)

	anonChalID, err := k.CreateChallenge(
		ctx,
		challenger,
		initID2,
		"Bad work anon",
		[]string{"evidence2"},
		math.ZeroInt(), // DREAM stake irrelevant for anonymous; SPARK is escrowed via bank
		true,
		"cosmos1payoutaddr",
		proof,
		nullifier,
	)
	require.NoError(t, err)

	// Verify SPARK was escrowed via bank keeper
	params, _ := k.Params.Get(ctx)
	expectedSpark := sdk.NewCoins(sdk.NewCoin("uspark", params.AnonymousChallengeSparkStake))
	require.Equal(t, expectedSpark, escrowedCoins)
	require.Equal(t, challenger, escrowSender)

	// Verify anonymous challenge
	anonChallenge, err := k.GetChallenge(ctx, anonChalID)
	require.NoError(t, err)
	require.True(t, anonChallenge.IsAnonymous)
	require.Equal(t, "cosmos1payoutaddr", anonChallenge.PayoutAddress)
	require.NotNil(t, anonChallenge.StakedSpark)
	require.True(t, anonChallenge.StakedSpark.Equal(params.AnonymousChallengeSparkStake))

	// Verify nullifier usage
	used, err := k.IsNullifierUsed(ctx, nullifier)
	require.NoError(t, err)
	require.True(t, used)

	// Test Case 3: Re-use nullifier (should fail)
	_, err = k.CreateChallenge(
		ctx,
		challenger,
		initID2, // Status is challenged, so this fail on status first anyway
		"Bad work anon 2",
		[]string{"evidence3"},
		math.ZeroInt(),
		true,
		"cosmos1payoutaddr",
		proof,
		nullifier,
	)
	require.Error(t, err)
}

func TestRespondToChallenge(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	projectID, _ := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr")),
		"Proj",
		"Desc",
		[]string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "500"},
	})
	k.MintDREAM(ctx, assignee, math.NewInt(1000))
	initID, _ := k.CreateInitiative(
		ctx,
		assignee, // acting as creator
		projectID,
		"Init",
		"D",
		[]string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	// Assign needed if creator != assignee but here we make them same for simplicity of creation
	// Actually creator != assignee usually.
	// CreateInitiative doesn't set Assignee.
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

	challenger := sdk.AccAddress([]byte("chal"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	// Create a juror
	juror := sdk.AccAddress([]byte("juror"))
	k.Member.Set(ctx, juror.String(), types.Member{
		Address:          juror.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"}, // Match initiative tag
	})

	// Update params for small jury
	params, _ := k.Params.Get(ctx)
	params.JurySize = 1
	params.MinJurorReputation = math.LegacyOneDec()
	k.Params.Set(ctx, params)

	k.MintDREAM(ctx, challenger, math.NewInt(1000))
	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Reason", nil, math.NewInt(50), false, "", nil, nil)
	require.NoError(t, err)

	// Valid Response
	err = k.RespondToChallenge(ctx, chalID, assignee, "My Defense", []string{"proof"})
	require.NoError(t, err)

	// Challenge should be in JURY_REVIEW (default triage result for non-empty response)
	challenge, _ := k.GetChallenge(ctx, chalID)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)

	// Verify Jury Review Created
	var jr types.JuryReview
	found := false
	err = k.JuryReview.Walk(ctx, nil, func(key uint64, val types.JuryReview) (bool, error) {
		if val.ChallengeId == chalID {
			jr = val
			found = true
			return true, nil // Stop iteration
		}
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, chalID, jr.ChallengeId)
	require.Equal(t, "My Defense", jr.AssigneeResponse)
}

func TestChallengeResponseDeadline(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Setup: Create a project and initiative
	projectID, err := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr1")),
		"Test Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, creator, math.NewInt(1000))

	initID, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Test Initiative",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	err = k.AssignInitiativeToMember(ctx, initID, creator)
	require.NoError(t, err)

	err = k.SubmitInitiativeWork(ctx, initID, creator, "Deliverable URI")
	require.NoError(t, err)

	// Create a challenger
	challenger := sdk.AccAddress([]byte("challenger"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	// Create a challenge
	chalID, err := k.CreateChallenge(
		ctx,
		challenger,
		initID,
		"Bad work",
		[]string{"evidence1"},
		math.NewInt(50),
		false,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)

	// Verify challenge was created with ResponseDeadline set
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)

	// ResponseDeadline should be set based on params
	params, _ := k.Params.Get(ctx)
	expectedDeadline := sdkCtx.BlockHeight() + (params.ChallengeResponseDeadlineEpochs * params.EpochBlocks)
	require.Equal(t, expectedDeadline, challenge.ResponseDeadline)
	require.Greater(t, challenge.ResponseDeadline, int64(0))
}

func TestChallengeAutoUpholdOnExpiration(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Setup: Create a project and initiative
	projectID, err := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr1")),
		"Test Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, creator, math.NewInt(1000))

	initID, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Test Initiative",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	err = k.AssignInitiativeToMember(ctx, initID, creator)
	require.NoError(t, err)

	err = k.SubmitInitiativeWork(ctx, initID, creator, "Deliverable URI")
	require.NoError(t, err)

	// Create a challenger
	challenger := sdk.AccAddress([]byte("challenger"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	// Set short deadline for testing
	params, _ := k.Params.Get(ctx)
	params.ChallengeResponseDeadlineEpochs = 1
	params.EpochBlocks = 10
	k.Params.Set(ctx, params)

	// Create a challenge
	chalID, err := k.CreateChallenge(
		ctx,
		challenger,
		initID,
		"Bad work",
		[]string{"evidence1"},
		math.NewInt(50),
		false,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)

	// Verify challenge is active
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)
	require.Equal(t, sdkCtx.BlockHeight()+10, challenge.ResponseDeadline) // 1 epoch * 10 blocks

	// Run EndBlocker before deadline - challenge should remain active
	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	challenge, err = k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)

	// Advance context past deadline
	newCtx := sdkCtx.WithBlockHeight(challenge.ResponseDeadline + 1)

	// Run EndBlocker after deadline - challenge should be auto-upheld
	err = k.EndBlocker(newCtx)
	require.NoError(t, err)

	// Verify challenge was upheld
	challenge, err = k.GetChallenge(newCtx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, challenge.Status)

	// Verify initiative was rejected
	init, err := k.GetInitiative(newCtx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, init.Status)
}

func TestChallengeResponsePreventsAutoUphold(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create a project and initiative
	projectID, err := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr1")),
		"Test Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, assignee, math.NewInt(1000))

	initID, err := k.CreateInitiative(
		ctx,
		assignee,
		projectID,
		"Test Initiative",
		"Desc",
		[]string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)
	require.NoError(t, err)

	err = k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	err = k.SubmitInitiativeWork(ctx, initID, assignee, "Deliverable URI")
	require.NoError(t, err)

	// Create a challenger
	challenger := sdk.AccAddress([]byte("challenger"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	// Create a juror for the jury selection
	juror := sdk.AccAddress([]byte("juror"))
	k.Member.Set(ctx, juror.String(), types.Member{
		Address:          juror.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag1": "100.0"},
	})

	// Set params for small jury
	params, _ := k.Params.Get(ctx)
	params.JurySize = 1
	params.MinJurorReputation = math.LegacyOneDec()
	params.ChallengeResponseDeadlineEpochs = 1
	params.EpochBlocks = 10
	k.Params.Set(ctx, params)

	// Create a challenge
	chalID, err := k.CreateChallenge(
		ctx,
		challenger,
		initID,
		"Bad work",
		[]string{"evidence1"},
		math.NewInt(50),
		false,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)

	// Verify challenge is active
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)

	// Assignee responds to the challenge (before deadline)
	err = k.RespondToChallenge(ctx, chalID, assignee, "My defense", []string{"evidence"})
	require.NoError(t, err)

	// Challenge should now be in JURY_REVIEW (not ACTIVE)
	challenge, err = k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)

	// Advance context past original deadline
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	newCtx := sdkCtx.WithBlockHeight(challenge.ResponseDeadline + 100)

	// Run EndBlocker - should NOT affect this challenge (it's already in JURY_REVIEW)
	err = k.EndBlocker(newCtx)
	require.NoError(t, err)

	// Verify challenge is still in JURY_REVIEW (not auto-upheld)
	challenge, err = k.GetChallenge(newCtx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)
}

// TestAnonymousChallengeUpheld verifies that when an anonymous challenge is upheld,
// the escrowed SPARK is returned to the challenger via bankKeeper.
func TestAnonymousChallengeUpheld(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup project + initiative
	projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
		[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
		StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, assignee, math.NewInt(1000))

	initID, err := k.CreateInitiative(ctx, assignee, projectID, "Init", "D", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(100))
	require.NoError(t, err)
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

	// Create anonymous challenge (SPARK escrow)
	anonChallenger := sdk.AccAddress([]byte("anon_alt_account"))
	fixture.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		return nil // Accept escrow
	}

	chalID, err := k.CreateChallenge(ctx, anonChallenger, initID, "Bad work", []string{"ev"},
		math.ZeroInt(), true, "", []byte("proof"), []byte("nullifier"))
	require.NoError(t, err)

	// Track SPARK return
	var returnedCoins sdk.Coins
	var returnRecipient sdk.AccAddress
	fixture.bankKeeper.SendCoinsFromModuleToAccountFn = func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		returnedCoins = amt
		returnRecipient = recipientAddr
		require.Equal(t, "rep", senderModule)
		return nil
	}

	// Uphold the challenge
	err = k.UpholdChallenge(ctx, chalID)
	require.NoError(t, err)

	// Verify SPARK was returned to the anonymous challenger
	params, _ := k.Params.Get(ctx)
	expectedSpark := sdk.NewCoins(sdk.NewCoin("uspark", params.AnonymousChallengeSparkStake))
	require.Equal(t, expectedSpark, returnedCoins)
	require.Equal(t, anonChallenger, returnRecipient)

	// Verify challenge status
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, challenge.Status)
}

// TestAnonymousChallengeRejected verifies that when an anonymous challenge is rejected,
// the escrowed SPARK is burned from the module account.
func TestAnonymousChallengeRejected(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup project + initiative
	projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
		[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address: creator.String(), DreamBalance: PtrInt(math.ZeroInt()),
		StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, creator, math.NewInt(1000))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Init", "D", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(100))
	require.NoError(t, err)
	k.AssignInitiativeToMember(ctx, initID, creator)
	k.SubmitInitiativeWork(ctx, initID, creator, "URI")

	// Create anonymous challenge (SPARK escrow)
	anonChallenger := sdk.AccAddress([]byte("anon_alt_account"))
	fixture.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		return nil
	}

	chalID, err := k.CreateChallenge(ctx, anonChallenger, initID, "Frivolous", []string{"ev"},
		math.ZeroInt(), true, "", []byte("proof"), []byte("nullifier"))
	require.NoError(t, err)

	// Track SPARK burn
	var burnedCoins sdk.Coins
	var burnModule string
	fixture.bankKeeper.BurnCoinsFn = func(ctx context.Context, moduleName string, amt sdk.Coins) error {
		burnedCoins = amt
		burnModule = moduleName
		return nil
	}

	// Reject the challenge
	err = k.RejectChallenge(ctx, chalID)
	require.NoError(t, err)

	// Verify SPARK was burned from module account
	params, _ := k.Params.Get(ctx)
	expectedSpark := sdk.NewCoins(sdk.NewCoin("uspark", params.AnonymousChallengeSparkStake))
	require.Equal(t, expectedSpark, burnedCoins)
	require.Equal(t, "rep", burnModule)

	// Verify challenge status
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, challenge.Status)

	// Verify initiative restored to IN_REVIEW
	init, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, init.Status)
}

func TestVerifyAnonymousEligibility(t *testing.T) {
	t.Run("empty proof returns false and error", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		valid, err := k.VerifyAnonymousEligibility(ctx, []byte{}, []byte("nullifier"))
		require.Error(t, err)
		require.False(t, valid)
		require.Contains(t, err.Error(), "empty proof")
	})

	t.Run("nil proof returns false and error", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		valid, err := k.VerifyAnonymousEligibility(ctx, nil, []byte("nullifier"))
		require.Error(t, err)
		require.False(t, valid)
		require.Contains(t, err.Error(), "empty proof")
	})

	t.Run("valid proof with nil voteKeeper dev mode returns true", func(t *testing.T) {
		// Construct a keeper with nil voteKeeper to test dev mode fallback
		encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
		addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
		storeKey := storetypes.NewKVStoreKey(types.StoreKey)
		storeService := runtime.NewKVStoreService(storeKey)
		ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
		authority := authtypes.NewModuleAddress(types.GovModuleName)

		k := keeper.NewKeeper(
			storeService,
			encCfg.Codec,
			addressCodec,
			authority,
			nil, // authKeeper
			&mockBankKeeper{},
			&mockCommonsKeeper{},
			&mockSeasonKeeper{},
			nil, // nil voteKeeper => dev mode
		)
		// InitGenesis so Params collection is populated
		genState := types.DefaultGenesis()
		err := k.InitGenesis(ctx, *genState)
		require.NoError(t, err)

		valid, verifyErr := k.VerifyAnonymousEligibility(ctx, []byte("valid_proof"), []byte("nullifier"))
		require.NoError(t, verifyErr)
		require.True(t, valid)
	})

	t.Run("valid proof with mock voteKeeper succeeds", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Default mockVoteKeeper accepts any non-empty proof
		valid, err := k.VerifyAnonymousEligibility(ctx, []byte("valid_proof"), []byte("nullifier"))
		require.NoError(t, err)
		require.True(t, valid)
	})

	t.Run("invalid proof with mock voteKeeper returns false", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Configure mock to reject the proof
		fixture.voteKeeper.VerifyMembershipProofFn = func(ctx context.Context, proof []byte, nullifier []byte) error {
			return fmt.Errorf("invalid membership proof")
		}

		valid, err := k.VerifyAnonymousEligibility(ctx, []byte("bad_proof"), []byte("nullifier"))
		require.Error(t, err)
		require.False(t, valid)
		require.Contains(t, err.Error(), "invalid membership proof")
	})
}

func TestHasActiveChallenges(t *testing.T) {
	// setupInitiativeWithChallenge is a helper that creates a project, initiative,
	// assignee, and challenger, returning the initiative ID and a function to create
	// a challenge on that initiative.
	setupInitiative := func(t *testing.T, fixture *fixture) uint64 {
		t.Helper()
		k := fixture.keeper
		ctx := fixture.ctx

		projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
			[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(1000), math.NewInt(100))
		require.NoError(t, err)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
		})
		k.MintDREAM(ctx, assignee, math.NewInt(1000))

		initID, err := k.CreateInitiative(ctx, assignee, projectID, "Init", "D", []string{"tag1"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(100))
		require.NoError(t, err)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

		return initID
	}

	t.Run("no challenges returns false", func(t *testing.T) {
		fixture := initFixture(t)
		initID := setupInitiative(t, fixture)

		hasActive, err := fixture.keeper.HasActiveChallenges(fixture.ctx, initID)
		require.NoError(t, err)
		require.False(t, hasActive)
	})

	t.Run("active challenge returns true", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx
		initID := setupInitiative(t, fixture)

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		_, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		hasActive, err := k.HasActiveChallenges(ctx, initID)
		require.NoError(t, err)
		require.True(t, hasActive)
	})

	t.Run("upheld challenge returns false", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx
		initID := setupInitiative(t, fixture)

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		// Uphold the challenge to resolve it
		err = k.UpholdChallenge(ctx, chalID)
		require.NoError(t, err)

		hasActive, err := k.HasActiveChallenges(ctx, initID)
		require.NoError(t, err)
		require.False(t, hasActive)
	})

	t.Run("rejected challenge returns false", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx
		initID := setupInitiative(t, fixture)

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		// Reject the challenge to resolve it
		err = k.RejectChallenge(ctx, chalID)
		require.NoError(t, err)

		hasActive, err := k.HasActiveChallenges(ctx, initID)
		require.NoError(t, err)
		require.False(t, hasActive)
	})

	t.Run("in jury review challenge returns true", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx
		initID := setupInitiative(t, fixture)

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		// Manually set the challenge status to IN_JURY_REVIEW
		challenge, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
		err = k.Challenge.Set(ctx, chalID, challenge)
		require.NoError(t, err)

		hasActive, err := k.HasActiveChallenges(ctx, initID)
		require.NoError(t, err)
		require.True(t, hasActive)
	})
}

func TestTriageChallenge(t *testing.T) {
	t.Run("empty response auto-upholds", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Setup a project and initiative so we can create a real challenge
		projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
			[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(1000), math.NewInt(100))
		require.NoError(t, err)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
		})
		k.MintDREAM(ctx, assignee, math.NewInt(1000))

		initID, err := k.CreateInitiative(ctx, assignee, projectID, "Init", "D", []string{"tag1"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(100))
		require.NoError(t, err)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		result, err := k.TriageChallenge(ctx, chalID, "", nil)
		require.NoError(t, err)
		require.Equal(t, keeper.TriageAutoApprove, result)
	})

	t.Run("non-empty response routes to jury", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
			[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(1000), math.NewInt(100))
		require.NoError(t, err)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
		})
		k.MintDREAM(ctx, assignee, math.NewInt(1000))

		initID, err := k.CreateInitiative(ctx, assignee, projectID, "Init", "D", []string{"tag1"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"", math.NewInt(100))
		require.NoError(t, err)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

		challenger := sdk.AccAddress([]byte("challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50), false, "", nil, nil)
		require.NoError(t, err)

		result, err := k.TriageChallenge(ctx, chalID, "I disagree with this challenge", []string{"my_evidence"})
		require.NoError(t, err)
		require.Equal(t, keeper.TriageRouteToJury, result)
	})
}

func TestMarkNullifierUsed(t *testing.T) {
	t.Run("initially not used", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		used, err := k.IsNullifierUsed(ctx, []byte("fresh_nullifier"))
		require.NoError(t, err)
		require.False(t, used)
	})

	t.Run("after marking is used", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		nullifier := []byte("my_nullifier")

		err := k.MarkNullifierUsed(ctx, nullifier)
		require.NoError(t, err)

		used, err := k.IsNullifierUsed(ctx, nullifier)
		require.NoError(t, err)
		require.True(t, used)
	})

	t.Run("different nullifier still returns false", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		err := k.MarkNullifierUsed(ctx, []byte("nullifier_A"))
		require.NoError(t, err)

		used, err := k.IsNullifierUsed(ctx, []byte("nullifier_B"))
		require.NoError(t, err)
		require.False(t, used)
	})
}

func TestEscalateChallengeToCommittee(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup project + initiative
	projectID, err := k.CreateProject(ctx, sdk.AccAddress([]byte("addr1")), "Proj", "D",
		[]string{"tag1"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
		StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"tag1": "100.0"},
	})
	k.MintDREAM(ctx, assignee, math.NewInt(1000))

	initID, err := k.CreateInitiative(ctx, assignee, projectID, "Init", "D", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(100))
	require.NoError(t, err)
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

	// Create a challenger and challenge
	challenger := sdk.AccAddress([]byte("challenger"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address: challenger.String(), DreamBalance: PtrInt(math.ZeroInt()),
		StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", []string{"evidence"}, math.NewInt(50), false, "", nil, nil)
	require.NoError(t, err)

	// Verify challenge is active before escalation
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, challenge.Status)

	// Escalate
	err = k.EscalateChallengeToCommittee(ctx, chalID, "My defense", []string{"defense_evidence"}, "insufficient_jurors")
	require.NoError(t, err)

	// Verify challenge status updated to IN_JURY_REVIEW
	challenge, err = k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)

	// Verify an ADJUDICATION interim was created
	var foundInterim types.Interim
	interimFound := false
	err = k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		if interim.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION && interim.ReferenceId == initID {
			foundInterim = interim
			interimFound = true
			return true, nil // stop iteration
		}
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, interimFound, "expected an ADJUDICATION interim to be created")
	require.Equal(t, types.InterimType_INTERIM_TYPE_ADJUDICATION, foundInterim.Type)
	require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, foundInterim.Complexity)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, foundInterim.Status)
	require.Contains(t, foundInterim.ReferenceType, fmt.Sprintf("Challenge %d escalated", chalID))
}

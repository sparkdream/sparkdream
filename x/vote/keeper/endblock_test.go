package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

// makeBaseProposal returns a minimal ACTIVE PUBLIC proposal with sensible defaults.
// Callers should override fields as needed before storing.
func makeBaseProposal(f *testFixture, opts []*types.VoteOption) types.VotingProposal {
	return types.VotingProposal{
		Id:             0,
		Title:          "Test Proposal",
		Options:        opts,
		Tally:          keeper.InitTallyForTest(opts),
		Status:         types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
		Outcome:        types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
		Visibility:     types.VisibilityLevel_VISIBILITY_PUBLIC,
		VotingStart:    0,
		VotingEnd:      100,
		RevealEnd:      100,
		EligibleVoters: 10,
		Quorum:         math.LegacyNewDecWithPrec(33, 2),  // 33%
		Threshold:      math.LegacyNewDecWithPrec(50, 2),  // 50%
		VetoThreshold:  math.LegacyNewDecWithPrec(334, 3), // 33.4%
	}
}

// ---------------------------------------------------------------------------
// Public proposal finalization
// ---------------------------------------------------------------------------

func TestEndBlockPublicProposalFinalized(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	// 4 votes for Yes out of 10 eligible (40% > 33% quorum)
	p.Tally[0].VoteCount = 4
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
}

// ---------------------------------------------------------------------------
// Outcome: PASSED
// ---------------------------------------------------------------------------

func TestEndBlockOutcomePassed(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Quorum = math.LegacyNewDecWithPrec(10, 2)    // 10%
	p.Threshold = math.LegacyNewDecWithPrec(50, 2)  // 50%
	p.EligibleVoters = 10

	// 6 votes for Yes, 1 for No (total 7, quorum = 70% > 10%; Yes ratio = 6/7 > 50%)
	p.Tally[0].VoteCount = 6 // Yes
	p.Tally[1].VoteCount = 1 // No
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_PASSED, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Outcome: REJECTED
// ---------------------------------------------------------------------------

func TestEndBlockOutcomeRejected(t *testing.T) {
	f := initTestFixture(t)

	// Use 3 standard options so votes split and no single option clears the threshold.
	threeOpts := []*types.VoteOption{
		{Id: 0, Label: "Option A", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		{Id: 1, Label: "Option B", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		{Id: 2, Label: "Option C", Role: types.OptionRole_OPTION_ROLE_STANDARD},
	}
	p := makeBaseProposal(f, threeOpts)
	p.Quorum = math.LegacyNewDecWithPrec(10, 2)    // 10%
	p.Threshold = math.LegacyNewDecWithPrec(50, 2)  // 50%
	p.EligibleVoters = 10

	// Votes: A=3, B=2, C=2 (total=7, quorum=70%>10%).
	// Winning option A has ratio 3/7 = 42.8% which is NOT > 50% threshold.
	p.Tally[0].VoteCount = 3 // Option A
	p.Tally[1].VoteCount = 2 // Option B
	p.Tally[2].VoteCount = 2 // Option C
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_REJECTED, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Outcome: QUORUM_NOT_MET
// ---------------------------------------------------------------------------

func TestEndBlockOutcomeQuorumNotMet(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Quorum = math.LegacyNewDecWithPrec(33, 2) // 33%
	p.EligibleVoters = 100

	// No votes cast at all (0/100 = 0% < 33%).
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Outcome: VETOED
// ---------------------------------------------------------------------------

func TestEndBlockOutcomeVetoed(t *testing.T) {
	f := initTestFixture(t)

	opts := f.optionsWithAbstainVeto()
	p := makeBaseProposal(f, opts)
	p.Quorum = math.LegacyNewDecWithPrec(10, 2)     // 10%
	p.VetoThreshold = math.LegacyNewDecWithPrec(33, 2) // 33%
	p.EligibleVoters = 10

	// 1 Yes, 0 No, 0 Abstain, 5 Veto
	// total=6, quorum=60%>10%, nonAbstain=6, vetoRatio=5/6=83.3% > 33%
	p.Tally[0].VoteCount = 1 // Yes
	p.Tally[1].VoteCount = 0 // No
	p.Tally[2].VoteCount = 0 // Abstain
	p.Tally[3].VoteCount = 5 // Veto
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_VETOED, updated.Outcome)
}

// ---------------------------------------------------------------------------
// All abstain => QUORUM_NOT_MET (nonAbstain == 0)
// ---------------------------------------------------------------------------

func TestEndBlockAllAbstain(t *testing.T) {
	f := initTestFixture(t)

	opts := f.optionsWithAbstainVeto()
	p := makeBaseProposal(f, opts)
	p.Quorum = math.LegacyNewDecWithPrec(10, 2) // 10%
	p.EligibleVoters = 10

	// 5 Abstain, nothing else. total=5, quorum=50%>10%, but nonAbstain=0 => QUORUM_NOT_MET.
	p.Tally[0].VoteCount = 0 // Yes
	p.Tally[1].VoteCount = 0 // No
	p.Tally[2].VoteCount = 5 // Abstain
	p.Tally[3].VoteCount = 0 // Veto
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Zero eligible voters => QUORUM_NOT_MET
// ---------------------------------------------------------------------------

func TestEndBlockZeroEligibleVoters(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.EligibleVoters = 0
	p.Tally[0].VoteCount = 3
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_QUORUM_NOT_MET, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Sealed proposal transitions to TALLYING (not FINALIZED)
// ---------------------------------------------------------------------------

func TestEndBlockSealedToTallying(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Visibility = types.VisibilityLevel_VISIBILITY_SEALED
	p.VotingEnd = 100
	p.RevealEnd = 200
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	// Advance past VotingEnd but before RevealEnd.
	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_TALLYING, updated.Status)
	// Outcome should still be unspecified (not finalized yet).
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED, updated.Outcome)
}

// ---------------------------------------------------------------------------
// Tallying proposal finalized after RevealEnd
// ---------------------------------------------------------------------------

func TestEndBlockTallyingFinalized(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING
	p.VotingEnd = 50
	p.RevealEnd = 100
	p.EligibleVoters = 10

	// 4 votes for Yes.
	p.Tally[0].VoteCount = 4
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	// Advance past RevealEnd.
	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
}

// ---------------------------------------------------------------------------
// Tie breaking: lowest option ID wins
// ---------------------------------------------------------------------------

func TestEndBlockTieBreaking(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Quorum = math.LegacyNewDecWithPrec(10, 2)    // 10%
	p.Threshold = math.LegacyNewDecWithPrec(50, 2)  // 50%
	p.EligibleVoters = 10

	// Equal votes on both standard options: 3 Yes (id=0), 3 No (id=1).
	// Tie-break: lower option ID wins => option 0 is winner.
	// winRatio = 3/6 = 50%, threshold is 50%. Code uses GT (>) not GTE (>=),
	// so 50% is NOT > 50% => outcome is REJECTED.
	p.Tally[0].VoteCount = 3 // Yes (id=0)
	p.Tally[1].VoteCount = 3 // No  (id=1)
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated, err := f.keeper.VotingProposal.Get(f.ctx, p.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated.Status)
	// With GT comparison, 50% is NOT > 50%, so it is rejected.
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_REJECTED, updated.Outcome)

	// Now test with a slightly lower threshold so the tie winner passes.
	p2 := makeBaseProposal(f, f.standardOptions())
	p2.Id = 1
	p2.Quorum = math.LegacyNewDecWithPrec(10, 2)    // 10%
	p2.Threshold = math.LegacyNewDecWithPrec(49, 2)  // 49%
	p2.EligibleVoters = 10
	p2.Tally[0].VoteCount = 3 // Yes (id=0)
	p2.Tally[1].VoteCount = 3 // No  (id=1)
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p2.Id, p2))

	// ProcessEndBlock already ran at height 101. Set to 102 to re-run (proposal 1 is new and ACTIVE).
	f.setBlockHeight(102)
	p2.VotingEnd = 101
	p2.RevealEnd = 101
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p2.Id, p2))
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	updated2, err := f.keeper.VotingProposal.Get(f.ctx, p2.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updated2.Status)
	// 50% > 49% threshold => PASSED. Tie-break selected option 0 (lower ID).
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_PASSED, updated2.Outcome)
}

// ---------------------------------------------------------------------------
// Event emission on finalization
// ---------------------------------------------------------------------------

func TestEndBlockProposalFinalizedEvent(t *testing.T) {
	f := initTestFixture(t)

	p := makeBaseProposal(f, f.standardOptions())
	p.Tally[0].VoteCount = 5
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, p.Id, p))

	// Reset event manager to isolate events.
	f.sdkCtx = f.sdkCtx.WithEventManager(sdk.NewEventManager())
	f.ctx = f.sdkCtx

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	events := f.sdkCtx.EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type == types.EventProposalFinalized {
			found = true
			var gotID, gotOutcome string
			for _, attr := range ev.Attributes {
				switch attr.Key {
				case types.AttributeProposalID:
					gotID = attr.Value
				case types.AttributeOutcome:
					gotOutcome = attr.Value
				}
			}
			require.Equal(t, "0", gotID)
			require.NotEmpty(t, gotOutcome)
		}
	}
	require.True(t, found, "expected proposal_finalized event to be emitted")
}

// ---------------------------------------------------------------------------
// Skips cancelled and finalized proposals
// ---------------------------------------------------------------------------

func TestEndBlockSkipsCancelledFinalized(t *testing.T) {
	f := initTestFixture(t)

	// Create a CANCELLED proposal.
	cancelled := makeBaseProposal(f, f.standardOptions())
	cancelled.Id = 0
	cancelled.Status = types.ProposalStatus_PROPOSAL_STATUS_CANCELLED
	cancelled.VotingEnd = 50
	cancelled.RevealEnd = 50
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, cancelled.Id, cancelled))

	// Create an already FINALIZED proposal.
	finalized := makeBaseProposal(f, f.standardOptions())
	finalized.Id = 1
	finalized.Status = types.ProposalStatus_PROPOSAL_STATUS_FINALIZED
	finalized.Outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_PASSED
	finalized.VotingEnd = 50
	finalized.RevealEnd = 50
	require.NoError(t, f.keeper.VotingProposal.Set(f.ctx, finalized.Id, finalized))

	// Reset events to check nothing is emitted.
	f.sdkCtx = f.sdkCtx.WithEventManager(sdk.NewEventManager())
	f.ctx = f.sdkCtx

	f.setBlockHeight(101)
	require.NoError(t, f.keeper.ProcessEndBlock(f.ctx))

	// Verify both proposals are unchanged.
	updatedCancelled, err := f.keeper.VotingProposal.Get(f.ctx, cancelled.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, updatedCancelled.Status)

	updatedFinalized, err := f.keeper.VotingProposal.Get(f.ctx, finalized.Id)
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_FINALIZED, updatedFinalized.Status)
	require.Equal(t, types.ProposalOutcome_PROPOSAL_OUTCOME_PASSED, updatedFinalized.Outcome)

	// No proposal_finalized events should have been emitted.
	events := f.sdkCtx.EventManager().Events()
	for _, ev := range events {
		require.NotEqual(t, types.EventProposalFinalized, ev.Type,
			"no proposal_finalized event should be emitted for cancelled or already-finalized proposals")
	}
}

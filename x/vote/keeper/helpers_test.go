package keeper_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

// ---------------------------------------------------------------------------
// Key format tests
// ---------------------------------------------------------------------------

func TestNullifierKeyFormat(t *testing.T) {
	tests := []struct {
		name       string
		proposalID uint64
		nullifier  []byte
		expected   string
	}{
		{
			name:       "basic key",
			proposalID: 42,
			nullifier:  []byte{0xde, 0xad, 0xbe, 0xef},
			expected:   "42/deadbeef",
		},
		{
			name:       "zero proposal ID",
			proposalID: 0,
			nullifier:  []byte{0x01, 0x02},
			expected:   "0/0102",
		},
		{
			name:       "large proposal ID",
			proposalID: 999999,
			nullifier:  genNullifier(7),
			expected:   fmt.Sprintf("999999/%s", hex.EncodeToString(genNullifier(7))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := keeper.NullifierKeyForTest(tc.proposalID, tc.nullifier)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestProposalNullifierKeyFormat(t *testing.T) {
	tests := []struct {
		name      string
		epoch     uint64
		nullifier []byte
		expected  string
	}{
		{
			name:      "basic key",
			epoch:     10,
			nullifier: []byte{0xca, 0xfe},
			expected:  "10/cafe",
		},
		{
			name:      "zero epoch",
			epoch:     0,
			nullifier: []byte{0xff},
			expected:  "0/ff",
		},
		{
			name:      "large epoch with 32 byte nullifier",
			epoch:     123456,
			nullifier: genNullifier(99),
			expected:  fmt.Sprintf("123456/%s", hex.EncodeToString(genNullifier(99))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := keeper.ProposalNullifierKeyForTest(tc.epoch, tc.nullifier)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestTleShareKeyFormat(t *testing.T) {
	tests := []struct {
		name      string
		validator string
		epoch     uint64
		expected  string
	}{
		{
			name:      "basic key",
			validator: "cosmos1abc",
			epoch:     5,
			expected:  "cosmos1abc/5",
		},
		{
			name:      "zero epoch",
			validator: "cosmos1xyz",
			epoch:     0,
			expected:  "cosmos1xyz/0",
		},
		{
			name:      "large epoch",
			validator: "val",
			epoch:     999999,
			expected:  "val/999999",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := keeper.TleShareKeyForTest(tc.validator, tc.epoch)
			require.Equal(t, tc.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Nullifier usage (indirect test via msg server)
// ---------------------------------------------------------------------------

func TestNullifierUsed(t *testing.T) {
	f := initTestFixture(t)

	// Register a voter so a proposal can be created (buildTreeSnapshot needs >= 1 active voter).
	f.registerVoter(t, f.member, genZkPubKey(1))

	// Create a public proposal.
	proposalID := f.createPublicProposal(t, f.member)

	nullifier := genNullifier(1)

	// First vote should succeed.
	_, err := f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 0,
		Proof:      []byte("stub-proof"),
	})
	require.NoError(t, err)

	// Second vote with the same nullifier should fail.
	_, err = f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member2,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		VoteOption: 1,
		Proof:      []byte("stub-proof"),
	})
	require.ErrorIs(t, err, types.ErrNullifierUsed)

	// A different nullifier on the same proposal should succeed.
	nullifier2 := genNullifier(2)
	_, err = f.msgServer.Vote(f.ctx, &types.MsgVote{
		Submitter:  f.member2,
		ProposalId: proposalID,
		Nullifier:  nullifier2,
		VoteOption: 1,
		Proof:      []byte("stub-proof"),
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ValidateProposalOptions
// ---------------------------------------------------------------------------

func TestValidateProposalOptions(t *testing.T) {
	f := initTestFixture(t)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)

	tests := []struct {
		name    string
		options []*types.VoteOption
		expErr  error
	}{
		{
			name: "valid: 2 standard options",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			},
			expErr: nil,
		},
		{
			name: "valid: standard + abstain + veto",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 2, Label: "Abstain", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
				{Id: 3, Label: "Veto", Role: types.OptionRole_OPTION_ROLE_VETO},
			},
			expErr: nil,
		},
		{
			name: "error: no standard option (abstain + veto only)",
			options: []*types.VoteOption{
				{Id: 0, Label: "Abstain", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
				{Id: 1, Label: "Veto", Role: types.OptionRole_OPTION_ROLE_VETO},
			},
			expErr: types.ErrNoStandardOption,
		},
		{
			name: "error: duplicate abstain",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 1, Label: "Abstain1", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
				{Id: 2, Label: "Abstain2", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
			},
			expErr: types.ErrDuplicateAbstainRole,
		},
		{
			name: "error: duplicate veto",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 1, Label: "Veto1", Role: types.OptionRole_OPTION_ROLE_VETO},
				{Id: 2, Label: "Veto2", Role: types.OptionRole_OPTION_ROLE_VETO},
			},
			expErr: types.ErrDuplicateVetoRole,
		},
		{
			name: "error: non-sequential IDs (gap)",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 2, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			},
			expErr: types.ErrInvalidVoteOptions,
		},
		{
			name: "error: non-sequential IDs (wrong start)",
			options: []*types.VoteOption{
				{Id: 1, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
				{Id: 2, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			},
			expErr: types.ErrInvalidVoteOptions,
		},
		{
			name: "error: count below min (1 option, min is 2)",
			options: []*types.VoteOption{
				{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			},
			expErr: types.ErrVoteOptionsOutOfRange,
		},
		{
			name: "error: count above max (11 options, max is 10)",
			options: func() []*types.VoteOption {
				opts := make([]*types.VoteOption, 11)
				for i := range opts {
					opts[i] = &types.VoteOption{
						Id:    uint32(i),
						Label: fmt.Sprintf("Option %d", i),
						Role:  types.OptionRole_OPTION_ROLE_STANDARD,
					}
				}
				return opts
			}(),
			expErr: types.ErrVoteOptionsOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.ValidateProposalOptionsForTest(f.keeper, params, tc.options)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// InitTally
// ---------------------------------------------------------------------------

func TestInitTally(t *testing.T) {
	t.Run("two options", func(t *testing.T) {
		opts := []*types.VoteOption{
			{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
		}
		tally := keeper.InitTallyForTest(opts)
		require.Len(t, tally, 2)
		for i, entry := range tally {
			require.Equal(t, uint32(i), entry.OptionId)
			require.Equal(t, uint64(0), entry.VoteCount)
		}
	})

	t.Run("four options with abstain and veto", func(t *testing.T) {
		opts := []*types.VoteOption{
			{Id: 0, Label: "Yes", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			{Id: 1, Label: "No", Role: types.OptionRole_OPTION_ROLE_STANDARD},
			{Id: 2, Label: "Abstain", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
			{Id: 3, Label: "Veto", Role: types.OptionRole_OPTION_ROLE_VETO},
		}
		tally := keeper.InitTallyForTest(opts)
		require.Len(t, tally, 4)
		for i, entry := range tally {
			require.Equal(t, opts[i].Id, entry.OptionId)
			require.Equal(t, uint64(0), entry.VoteCount)
		}
	})

	t.Run("empty options", func(t *testing.T) {
		tally := keeper.InitTallyForTest(nil)
		require.Empty(t, tally)
	})
}

// ---------------------------------------------------------------------------
// BuildTreeSnapshot (tested indirectly via proposal creation)
// ---------------------------------------------------------------------------

func TestBuildTreeSnapshot(t *testing.T) {
	f := initTestFixture(t)

	// Register 3 active voters.
	f.registerVoter(t, f.member, genZkPubKey(10))
	f.registerVoter(t, f.member2, genZkPubKey(11))
	f.registerVoter(t, f.validator, genZkPubKey(12))

	// Deactivate one voter.
	_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
		Voter: f.member2,
	})
	require.NoError(t, err)

	// Create a proposal. The snapshot should only include 2 active voters.
	proposalID := f.createPublicProposal(t, f.member)

	// Verify EligibleVoters count on the proposal.
	proposal, err := f.keeper.VotingProposal.Get(f.ctx, proposalID)
	require.NoError(t, err)
	require.Equal(t, uint64(2), proposal.EligibleVoters)
	require.NotEmpty(t, proposal.MerkleRoot)
}

func TestBuildTreeSnapshotNoVoters(t *testing.T) {
	f := initTestFixture(t)

	// Creating a proposal with no voters should fail.
	_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
		Proposer:   f.member,
		Title:      "Test",
		Options:    f.standardOptions(),
		Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
		Deposit:    types.DefaultParams().MinProposalDeposit,
	})
	require.ErrorIs(t, err, types.ErrNoEligibleVoters)
}

// ---------------------------------------------------------------------------
// IsZkPubKeyUnique (tested indirectly via msg server)
// ---------------------------------------------------------------------------

func TestIsZkPubKeyUnique(t *testing.T) {
	f := initTestFixture(t)

	sharedKey := genZkPubKey(100)

	// Register first voter with a key.
	f.registerVoter(t, f.member, sharedKey)

	// Register second voter with the SAME key — should fail with ErrDuplicatePublicKey.
	_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
		Voter:       f.member2,
		ZkPublicKey: sharedKey,
	})
	require.ErrorIs(t, err, types.ErrDuplicatePublicKey)

	// Register second voter with a DIFFERENT key — should succeed.
	f.registerVoter(t, f.member2, genZkPubKey(101))
}

func TestIsZkPubKeyUniqueRotation(t *testing.T) {
	f := initTestFixture(t)

	key1 := genZkPubKey(200)
	key2 := genZkPubKey(201)

	// Register two voters with distinct keys.
	f.registerVoter(t, f.member, key1)
	f.registerVoter(t, f.member2, key2)

	// member tries to rotate to member2's key — should fail.
	_, err := f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
		Voter:          f.member,
		NewZkPublicKey: key2,
	})
	require.ErrorIs(t, err, types.ErrDuplicatePublicKey)

	// member rotates to a fresh key — should succeed.
	_, err = f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
		Voter:          f.member,
		NewZkPublicKey: genZkPubKey(202),
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ComputeCommitmentHash
// ---------------------------------------------------------------------------

func TestComputeCommitmentHash(t *testing.T) {
	// Use simple byte salts that are known to produce distinct MiMC field elements.
	salt1 := make([]byte, 32)
	salt1[31] = 1
	salt2 := make([]byte, 32)
	salt2[31] = 2

	t.Run("deterministic", func(t *testing.T) {
		h1 := keeper.ComputeCommitmentHashForTest(1, salt1)
		h2 := keeper.ComputeCommitmentHashForTest(1, salt1)
		require.Equal(t, h1, h2)
		require.NotEmpty(t, h1)
	})

	t.Run("different vote option changes hash", func(t *testing.T) {
		h1 := keeper.ComputeCommitmentHashForTest(0, salt1)
		h2 := keeper.ComputeCommitmentHashForTest(1, salt1)
		require.NotEqual(t, h1, h2)
	})

	t.Run("different salt changes hash", func(t *testing.T) {
		h1 := keeper.ComputeCommitmentHashForTest(1, salt1)
		h2 := keeper.ComputeCommitmentHashForTest(1, salt2)
		require.NotEqual(t, h1, h2)
	})
}

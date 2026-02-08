package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestGetReputationScores(t *testing.T) {
	tests := []struct {
		name             string
		setupMember      bool
		memberAddr       sdk.AccAddress
		reputationScores map[string]string
		expectedScores   map[string]string
		expectError      bool
	}{
		{
			name:        "member not found",
			setupMember: false,
			memberAddr:  TestAddrMember1,
			expectError: true,
		},
		{
			name:             "member with empty reputation",
			setupMember:      true,
			memberAddr:       TestAddrMember1,
			reputationScores: map[string]string{},
			expectedScores:   map[string]string{},
			expectError:      false,
		},
		{
			name:        "member with single tag",
			setupMember: true,
			memberAddr:  TestAddrMember1,
			reputationScores: map[string]string{
				"backend": "100.5",
			},
			expectedScores: map[string]string{
				"backend": "100.5",
			},
			expectError: false,
		},
		{
			name:        "member with multiple tags",
			setupMember: true,
			memberAddr:  TestAddrMember1,
			reputationScores: map[string]string{
				"backend":  "150.0",
				"frontend": "75.25",
				"design":   "50.0",
			},
			expectedScores: map[string]string{
				"backend":  "150.0",
				"frontend": "75.25",
				"design":   "50.0",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)

			if tt.setupMember {
				member := types.Member{
					Address:          tt.memberAddr.String(),
					DreamBalance:     PtrInt(math.NewInt(1000)),
					StakedDream:      PtrInt(math.ZeroInt()),
					TrustLevel:       types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
					ReputationScores: tt.reputationScores,
				}
				err := f.keeper.Member.Set(f.ctx, tt.memberAddr.String(), member)
				require.NoError(t, err)
			}

			scores, err := f.keeper.GetReputationScores(f.ctx, tt.memberAddr.String())

			if tt.expectError {
				require.Error(t, err)
				require.Equal(t, types.ErrMemberNotFound, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, len(tt.expectedScores), len(scores))
				for tag, expectedScore := range tt.expectedScores {
					actualScore, exists := scores[tag]
					require.True(t, exists, "expected tag %s to exist", tag)
					require.Equal(t, expectedScore, actualScore)
				}
			}
		})
	}
}

func TestGetReputationScores_ReturnsCopy(t *testing.T) {
	f := initFixture(t)

	// Setup member with reputation
	member := types.Member{
		Address:      TestAddrMember1.String(),
		DreamBalance: PtrInt(math.NewInt(1000)),
		StakedDream:  PtrInt(math.ZeroInt()),
		TrustLevel:   types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		ReputationScores: map[string]string{
			"backend": "100.0",
		},
	}
	err := f.keeper.Member.Set(f.ctx, TestAddrMember1.String(), member)
	require.NoError(t, err)

	// Get scores and modify the returned map
	scores, err := f.keeper.GetReputationScores(f.ctx, TestAddrMember1.String())
	require.NoError(t, err)
	scores["backend"] = "999.0"
	scores["newTag"] = "50.0"

	// Original member should be unchanged
	originalMember, err := f.keeper.Member.Get(f.ctx, TestAddrMember1.String())
	require.NoError(t, err)
	require.Equal(t, "100.0", originalMember.ReputationScores["backend"])
	_, exists := originalMember.ReputationScores["newTag"]
	require.False(t, exists, "newTag should not exist in original")
}

func TestArchiveSeasonalReputation(t *testing.T) {
	tests := []struct {
		name                  string
		setupMember           bool
		memberAddr            sdk.AccAddress
		seasonalScores        map[string]string
		existingLifetime      map[string]string
		expectedArchived      map[string]string
		expectedNewLifetime   map[string]string
		expectedSeasonalAfter map[string]string
		expectError           bool
	}{
		{
			name:        "member not found",
			setupMember: false,
			memberAddr:  TestAddrMember1,
			expectError: true,
		},
		{
			name:                  "empty seasonal scores",
			setupMember:           true,
			memberAddr:            TestAddrMember1,
			seasonalScores:        map[string]string{},
			existingLifetime:      nil,
			expectedArchived:      map[string]string{},
			expectedNewLifetime:   map[string]string{},
			expectedSeasonalAfter: map[string]string{},
			expectError:           false,
		},
		{
			name:        "archive single tag - no existing lifetime",
			setupMember: true,
			memberAddr:  TestAddrMember1,
			seasonalScores: map[string]string{
				"backend": "100.0",
			},
			existingLifetime: nil,
			expectedArchived: map[string]string{
				"backend": "100.0",
			},
			expectedNewLifetime: map[string]string{
				"backend": "100.000000000000000000",
			},
			expectedSeasonalAfter: map[string]string{},
			expectError:           false,
		},
		{
			name:        "archive adds to existing lifetime",
			setupMember: true,
			memberAddr:  TestAddrMember1,
			seasonalScores: map[string]string{
				"backend": "50.5",
			},
			existingLifetime: map[string]string{
				"backend": "100.0",
			},
			expectedArchived: map[string]string{
				"backend": "50.5",
			},
			expectedNewLifetime: map[string]string{
				"backend": "150.500000000000000000",
			},
			expectedSeasonalAfter: map[string]string{},
			expectError:           false,
		},
		{
			name:        "archive multiple tags - mixed existing",
			setupMember: true,
			memberAddr:  TestAddrMember1,
			seasonalScores: map[string]string{
				"backend":  "100.0",
				"frontend": "50.0",
				"design":   "25.0",
			},
			existingLifetime: map[string]string{
				"backend": "200.0", // Existing tag
				// frontend is new
				"infra": "75.0", // Unrelated tag stays
			},
			expectedArchived: map[string]string{
				"backend":  "100.0",
				"frontend": "50.0",
				"design":   "25.0",
			},
			expectedNewLifetime: map[string]string{
				"backend":  "300.000000000000000000",
				"frontend": "50.000000000000000000",
				"design":   "25.000000000000000000",
				"infra":    "75.0", // Unchanged
			},
			expectedSeasonalAfter: map[string]string{},
			expectError:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)

			if tt.setupMember {
				member := types.Member{
					Address:            tt.memberAddr.String(),
					DreamBalance:       PtrInt(math.NewInt(1000)),
					StakedDream:        PtrInt(math.ZeroInt()),
					TrustLevel:         types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
					ReputationScores:   tt.seasonalScores,
					LifetimeReputation: tt.existingLifetime,
				}
				err := f.keeper.Member.Set(f.ctx, tt.memberAddr.String(), member)
				require.NoError(t, err)
			}

			archivedScores, err := f.keeper.ArchiveSeasonalReputation(f.ctx, tt.memberAddr.String())

			if tt.expectError {
				require.Error(t, err)
				require.Equal(t, types.ErrMemberNotFound, err)
			} else {
				require.NoError(t, err)

				// Check returned archived scores
				require.Equal(t, len(tt.expectedArchived), len(archivedScores))
				for tag, expectedScore := range tt.expectedArchived {
					actualScore, exists := archivedScores[tag]
					require.True(t, exists, "expected archived tag %s to exist", tag)
					require.Equal(t, expectedScore, actualScore)
				}

				// Verify member state was updated
				member, err := f.keeper.Member.Get(f.ctx, tt.memberAddr.String())
				require.NoError(t, err)

				// Seasonal scores should be cleared
				require.Equal(t, len(tt.expectedSeasonalAfter), len(member.ReputationScores),
					"seasonal scores should be cleared")

				// Lifetime should be updated
				require.Equal(t, len(tt.expectedNewLifetime), len(member.LifetimeReputation))
				for tag, expectedScore := range tt.expectedNewLifetime {
					actualScore, exists := member.LifetimeReputation[tag]
					require.True(t, exists, "expected lifetime tag %s to exist", tag)
					require.Equal(t, expectedScore, actualScore)
				}
			}
		})
	}
}

func TestArchiveSeasonalReputation_SkipsMalformedScores(t *testing.T) {
	f := initFixture(t)

	// Setup member with mix of valid and malformed scores
	member := types.Member{
		Address:      TestAddrMember1.String(),
		DreamBalance: PtrInt(math.NewInt(1000)),
		StakedDream:  PtrInt(math.ZeroInt()),
		TrustLevel:   types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		ReputationScores: map[string]string{
			"valid":     "100.0",
			"malformed": "not-a-number",
			"valid2":    "50.5",
		},
		LifetimeReputation: nil,
	}
	err := f.keeper.Member.Set(f.ctx, TestAddrMember1.String(), member)
	require.NoError(t, err)

	archivedScores, err := f.keeper.ArchiveSeasonalReputation(f.ctx, TestAddrMember1.String())
	require.NoError(t, err)

	// All scores returned in archived (including malformed)
	require.Equal(t, 3, len(archivedScores))

	// Only valid scores should be added to lifetime
	updatedMember, err := f.keeper.Member.Get(f.ctx, TestAddrMember1.String())
	require.NoError(t, err)

	// Valid scores should be in lifetime
	require.Contains(t, updatedMember.LifetimeReputation, "valid")
	require.Contains(t, updatedMember.LifetimeReputation, "valid2")

	// Malformed should NOT be in lifetime (it was skipped)
	_, hasMalformed := updatedMember.LifetimeReputation["malformed"]
	require.False(t, hasMalformed, "malformed score should not be added to lifetime")
}

func TestGetCompletedInitiativesCount(t *testing.T) {
	tests := []struct {
		name            string
		setupMember     bool
		memberAddr      sdk.AccAddress
		initiativeCount uint32
		expectedCount   uint64
		expectError     bool
	}{
		{
			name:        "member not found",
			setupMember: false,
			memberAddr:  TestAddrMember1,
			expectError: true,
		},
		{
			name:            "zero initiatives",
			setupMember:     true,
			memberAddr:      TestAddrMember1,
			initiativeCount: 0,
			expectedCount:   0,
			expectError:     false,
		},
		{
			name:            "some initiatives",
			setupMember:     true,
			memberAddr:      TestAddrMember1,
			initiativeCount: 5,
			expectedCount:   5,
			expectError:     false,
		},
		{
			name:            "many initiatives",
			setupMember:     true,
			memberAddr:      TestAddrMember1,
			initiativeCount: 100,
			expectedCount:   100,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)

			if tt.setupMember {
				member := types.Member{
					Address:                   tt.memberAddr.String(),
					DreamBalance:              PtrInt(math.NewInt(1000)),
					StakedDream:               PtrInt(math.ZeroInt()),
					TrustLevel:                types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
					CompletedInitiativesCount: tt.initiativeCount,
				}
				err := f.keeper.Member.Set(f.ctx, tt.memberAddr.String(), member)
				require.NoError(t, err)
			}

			count, err := f.keeper.GetCompletedInitiativesCount(f.ctx, tt.memberAddr.String())

			if tt.expectError {
				require.Error(t, err)
				require.Equal(t, types.ErrMemberNotFound, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedCount, count)
			}
		})
	}
}

func TestFormatTagCount(t *testing.T) {
	// This is a simple helper function test
	// The function is private but we can test it indirectly through the events
	// For now, we just verify it doesn't panic with various inputs
	f := initFixture(t)

	member := types.Member{
		Address:      TestAddrMember1.String(),
		DreamBalance: PtrInt(math.NewInt(1000)),
		StakedDream:  PtrInt(math.ZeroInt()),
		TrustLevel:   types.TrustLevel_TRUST_LEVEL_PROVISIONAL,
		ReputationScores: map[string]string{
			"tag1": "10.0",
			"tag2": "20.0",
			"tag3": "30.0",
		},
	}
	err := f.keeper.Member.Set(f.ctx, TestAddrMember1.String(), member)
	require.NoError(t, err)

	// Archive should not panic
	_, err = f.keeper.ArchiveSeasonalReputation(f.ctx, TestAddrMember1.String())
	require.NoError(t, err)
}

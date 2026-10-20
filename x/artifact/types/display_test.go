package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestScheduleValidationAndStage(t *testing.T) {
	p := types.DefaultParams()
	stages := []types.MetadataStage{
		{FromTime: 110, Metadata: types.TokenMetadata{Name: "egg"}},
		{FromTime: 120, Metadata: types.TokenMetadata{Name: "chick"}},
	}
	require.NoError(t, types.ValidateSchedule(stages, 100, p))
	require.ErrorIs(t, types.ValidateSchedule(stages, 110, p), types.ErrInvalidSchedule, "first stage not in the future")
	require.ErrorIs(t, types.ValidateSchedule([]types.MetadataStage{stages[1], stages[0]}, 100, p), types.ErrInvalidSchedule, "not increasing")
	bad := []types.MetadataStage{{FromTime: 110, Metadata: types.TokenMetadata{Uri: "data:text/plain,x"}}}
	require.ErrorIs(t, types.ValidateSchedule(bad, 100, p), types.ErrInlineData)
	p.MaxScheduleStages = 1
	require.ErrorIs(t, types.ValidateSchedule(stages, 100, p), types.ErrInvalidSchedule, "cap")

	require.Equal(t, -1, types.StageInEffect(stages, 105))
	require.Equal(t, 0, types.StageInEffect(stages, 110))
	require.Equal(t, 1, types.StageInEffect(stages, 500))
}

func TestStoredDisplayPriority(t *testing.T) {
	tok := types.Token{
		Metadata: types.TokenMetadata{Name: "base"},
		Schedule: []types.MetadataStage{{FromTime: 200, Metadata: types.TokenMetadata{Name: "later"}}},
	}
	m, src := types.StoredDisplay(tok, 100)
	require.Equal(t, "base", m.Name)
	require.Equal(t, types.DisplayStored, src)
	m, src = types.StoredDisplay(tok, 200)
	require.Equal(t, "later", m.Name)
	require.Equal(t, types.DisplaySchedule, src)

	tok.MediaVersions = []types.TokenMetadata{{Name: "v1"}, {Name: "v2"}}
	tok.DisplayedVersion = 2
	m, src = types.StoredDisplay(tok, 200)
	require.Equal(t, "v2", m.Name)
	require.Equal(t, types.DisplayMediaVersion, src)

	tok.Unrevealed = true
	m, src = types.StoredDisplay(tok, 200)
	require.True(t, m.IsEmpty())
	require.Equal(t, types.DisplayUnrevealed, src)

	require.Equal(t, uint32(8), types.LabelTokenAll(types.Token{Schedule: []types.MetadataStage{{Metadata: types.TokenMetadata{Uri: "ipfs://x"}}}}))
	require.Equal(t, uint32(0), types.LabelTokenAll(types.Token{Metadata: types.TokenMetadata{Name: "n"}}))
	require.True(t, types.IsExpired(types.Token{ExpiresAt: 5}, 5))
	require.False(t, types.IsExpired(types.Token{ExpiresAt: 0}, 5))
}

func TestClassPolicyValidation(t *testing.T) {
	require.ErrorIs(t, types.ClassFlags{Mutual: true}.Validate(), types.ErrInvalidFlags)
	require.ErrorIs(t, types.ClassFlags{Mutual: true, Transferable: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}.Validate(), types.ErrInvalidFlags)
	require.NoError(t, types.ClassFlags{Mutual: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}.Validate())
	require.ErrorIs(t, types.ClassFlags{RedeemedTransferable: true, Transferable: true}.Validate(), types.ErrInvalidFlags)
	require.ErrorIs(t, types.ClassFlags{RedeemedTransferable: true, Redeemable: true}.Validate(), types.ErrInvalidFlags)
	require.NoError(t, types.ClassFlags{RedeemedTransferable: true, Redeemable: true, Transferable: true}.Validate())

	require.NoError(t, types.ExpiryPolicy{}.Validate())
	require.ErrorIs(t, types.ExpiryPolicy{Term: 5}.Validate(), types.ErrInvalidExpiry)
	require.ErrorIs(t, types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK}.Validate(), types.ErrInvalidExpiry)
	require.ErrorIs(t, types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: types.MaxTokenTerm + 1}.Validate(), types.ErrInvalidExpiry)
	require.NoError(t, types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: types.MinTokenTerm}.Validate())

	require.NoError(t, types.ValidateRevealInput(types.RevealPolicy{}, 100))
	require.ErrorIs(t, types.ValidateRevealInput(types.RevealPolicy{Deadline: 5}, 100), types.ErrInvalidReveal)
	require.ErrorIs(t, types.ValidateRevealInput(types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: "zz"}, 100), types.ErrInvalidReveal)
	commit := types.SeedCommitment([]byte("s"))
	require.ErrorIs(t, types.ValidateRevealInput(types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: commit, Deadline: 100 + types.MinRevealWindow - 1}, 100), types.ErrInvalidReveal)
	require.ErrorIs(t, types.ValidateRevealInput(types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: commit, Deadline: 100 + types.MinRevealWindow, Seed: "aa"}, 100), types.ErrInvalidReveal)
	require.NoError(t, types.ValidateRevealInput(types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: commit, Deadline: 100 + types.MinRevealWindow}, 100))
}

func TestParamsBoundsForNewFields(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	p.MaxIssueBatchSize = types.MaxIssueBatchSizeCeiling + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxIssueBatchSize = 0
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxScheduleStages = types.HardMaxScheduleStages + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxMediaProposalsPerToken = types.HardMaxMediaProposals + 1
	require.Error(t, p.Validate())
	p = types.DefaultParams()
	p.MaxMediaVersionsPerToken = types.HardMaxMediaVersions + 1
	require.Error(t, p.Validate())
	// renderers_enabled is operational and survives the round trip.
	op := types.DefaultParams().OperationalParams()
	require.True(t, op.RenderersEnabled)
	op.RenderersEnabled = false
	require.False(t, types.DefaultParams().ApplyOperationalParams(op).RenderersEnabled)
}

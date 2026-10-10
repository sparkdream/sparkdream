package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestDefaultParamsValid(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())
	require.NoError(t, types.DefaultParams().OperationalParams().Validate())
}

func TestParamsBounds(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *types.Params)
	}{
		{"royalty ceiling", func(p *types.Params) { p.MaxRoyaltyBps = types.MaxRoyaltyBpsCeiling + 1 }},
		{"sale fee ceiling", func(p *types.Params) { p.SaleFeeBps = types.MaxSaleFeeBpsCeiling + 1 }},
		{"zero deposit", func(p *types.Params) { p.TokenDeposit = p.TokenDeposit.MulRaw(0) }},
		{"zero creation fee", func(p *types.Params) { p.ClassCreationFee = p.ClassCreationFee.MulRaw(0) }},
		{"zero inbox fee", func(p *types.Params) { p.InboxFee = p.InboxFee.MulRaw(0) }},
		{"huge pair cap", func(p *types.Params) { p.MaxPendingPerPair = types.MaxPendingPerPairCeiling + 1 }},
		{"tiny inbox cap", func(p *types.Params) { p.MaxInboxPerRecipient = types.MinInboxPerRecipient - 1 }},
		{"pending ttl", func(p *types.Params) { p.PendingTtl = types.MinPendingTTL - 1 }},
		{"listing duration", func(p *types.Params) { p.MaxListingDuration = types.MaxMaxListingDuration + 1 }},
		{"batch", func(p *types.Params) { p.MaxBatchSize = 0 }},
		{"expirations", func(p *types.Params) { p.MaxExpirationsPerBlock = 1 }},
		{"hide expiry", func(p *types.Params) { p.HideExpiryBlocks = 0 }},
		{"no schemes", func(p *types.Params) { p.AllowedUriSchemes = nil }},
		{"http scheme", func(p *types.Params) { p.AllowedUriSchemes = []string{"http"} }},
		{"data scheme", func(p *types.Params) { p.AllowedUriSchemes = []string{"ipfs", "data"} }},
		{"uppercase scheme", func(p *types.Params) { p.AllowedUriSchemes = []string{"IPFS"} }},
		{"dup scheme", func(p *types.Params) { p.AllowedUriSchemes = []string{"ipfs", "ipfs"} }},
		{"unspecified policy", func(p *types.Params) { p.DefaultReceivePolicy = types.ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED }},
		{"trust level", func(p *types.Params) { p.MinTrustLevelCreateClass = 5 }},
		{"no classes", func(p *types.Params) { p.MaxClassesPerCreator = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mut(&p)
			require.Error(t, p.Validate())
		})
	}
}

func TestApplyOperationalParams(t *testing.T) {
	p := types.DefaultParams()
	op := p.OperationalParams()
	op.PendingTtl = 99
	op.MarketEnabled = false
	got := p.ApplyOperationalParams(op)
	require.Equal(t, int64(99), got.PendingTtl)
	require.False(t, got.MarketEnabled)
	require.Equal(t, p.MaxRoyaltyBps, got.MaxRoyaltyBps)
	require.Equal(t, op, got.OperationalParams())
}

func TestValidateURI(t *testing.T) {
	p := types.DefaultParams()
	ok := []string{
		"", "ipfs://bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
		"ipfs://QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG/1.png",
		"ar://Abc-def_123", "https://phoenix.example/aurora.png?v=1",
	}
	for _, u := range ok {
		require.NoError(t, types.ValidateURI("uri", u, p), u)
	}
	bad := []string{
		"http://phoenix.example/x", "javascript:alert(1)", "data:image/png;base64,AA", "file:///etc/passwd",
		"https://user:pw@phoenix.example/x", "https://127.0.0.1/x", "https://[::1]/x", "https://phoenix.example/a b",
		"https://phöenix.example/x", "HTTPS://phoenix.example/x", "ipfs:bafy", "/relative/path", "https:///nohost",
		"https://" + strings.Repeat("a", 600),
	}
	for _, u := range bad {
		require.Error(t, types.ValidateURI("uri", u, p), u)
	}
}

func TestValidateText(t *testing.T) {
	require.NoError(t, types.ValidateText("d", "line one\nline two\ttab", 100, true))
	require.Error(t, types.ValidateText("n", "line\nbreak", 100, false))
	require.Error(t, types.ValidateText("n", "bell\a", 100, true))
	require.Error(t, types.ValidateText("n", string([]byte{0xff, 0xfe}), 100, false))
	require.Error(t, types.ValidateText("n", "abcdef", 5, false))
	require.ErrorIs(t, types.ValidateText("d", "embedded data:,hello", 100, true), types.ErrInlineData)
	// Prose that merely mentions "metadata:" is fine.
	require.NoError(t, types.ValidateText("d", "metadata: none", 100, true))
}

func TestValidateTokenMetadataAndHash(t *testing.T) {
	p := types.DefaultParams()
	m := types.TokenMetadata{Name: "Aurora #1", Uri: "ipfs://bafyaurora", UriHash: strings.Repeat("ab", 32),
		Attributes: []types.Attribute{{Key: "edition", Value: "1"}, {Key: "season_3", Value: "yes"}}}
	require.NoError(t, types.ValidateTokenMetadata(m, p))

	h1 := types.MetadataHash(m)
	require.Len(t, h1, 64)
	require.Equal(t, h1, types.MetadataHash(m), "deterministic")
	m2 := m
	m2.Name = "Aurora #2"
	require.NotEqual(t, h1, types.MetadataHash(m2))

	tooMany := types.TokenMetadata{}
	for i := 0; i <= int(p.MaxAttributes); i++ {
		tooMany.Attributes = append(tooMany.Attributes, types.Attribute{Key: "k" + strings.Repeat("x", i), Value: "v"})
	}
	require.Error(t, types.ValidateTokenMetadata(tooMany, p))
	require.Error(t, types.ValidateTokenMetadata(types.TokenMetadata{UriHash: "XYZ"}, p))
}

func TestValidateSymbolAndLabels(t *testing.T) {
	require.NoError(t, types.ValidateSymbol("", 16))
	require.NoError(t, types.ValidateSymbol("PHX2", 16))
	require.Error(t, types.ValidateSymbol("phx", 16))
	require.Error(t, types.ValidateSymbol("PHOENIXAURORAZENITH", 16))

	require.Equal(t, uint32(0), types.LabelToken(types.TokenMetadata{}))
	require.Equal(t, uint32(8), types.LabelToken(types.TokenMetadata{Uri: "ipfs://x"}))
	require.Equal(t, uint32(8), types.LabelClass(types.Class{TokenUriBase: "ipfs://x/"}))
}

func TestMintOrigin(t *testing.T) {
	require.Equal(t, "class/42", types.MintOrigin(42))
}

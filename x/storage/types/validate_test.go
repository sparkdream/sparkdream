package types_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/storage/types"
)

func TestValidateSourceURL(t *testing.T) {
	ok := []string{
		"https://files.example/a.png",
		"https://files.example:8443/a%20b.png?token=abc&x=1",
		"https://[2001:db8::1]/a",
	}
	bad := []string{
		"",
		"http://files.example/a.png",
		"ftp://files.example/a.png",
		"https:///a.png",
		"https://user:pw@files.example/a.png",
		"https://files.example/a.png#frag",
		"https://files.example/a b.png",
		"https://files.example/a\"b",
		"https://files.example/<a>",
		"https://files.example/a\nb",
		"https://files.example/é",
		"files.example/a.png",
		"https://files.example/" + strings.Repeat("a", 600),
	}
	for _, u := range ok {
		require.NoError(t, types.ValidateSourceURL(u, 512), u)
	}
	for _, u := range bad {
		require.ErrorIs(t, types.ValidateSourceURL(u, 512), types.ErrInvalidURL, u)
	}
}

func TestValidateHashAndLocator(t *testing.T) {
	h := strings.Repeat("ab", 32)
	require.NoError(t, types.ValidateHash(h))
	for _, bad := range []string{"", h[:63], h + "a", strings.ToUpper(h), strings.Repeat("zz", 32)} {
		require.ErrorIs(t, types.ValidateHash(bad), types.ErrInvalidHash)
	}
	loc := strings.Repeat("aB3_-", 8) + "xyz"
	require.NoError(t, types.ValidateLocator(types.LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX, loc))
	for _, bad := range []string{"", loc[:42], loc + "a", strings.Repeat("+", 43), strings.Repeat("/", 43)} {
		require.ErrorIs(t, types.ValidateLocator(types.LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX, bad), types.ErrInvalidLocator)
	}
	require.ErrorIs(t, types.ValidateLocator(types.LocatorFormat_LOCATOR_FORMAT_UNSPECIFIED, loc), types.ErrInvalidLocator)
}

func TestParamsValidate(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())
	cases := map[string]func(p *types.Params){
		"zero window":                   func(p *types.Params) { p.SettleWindowBlocks = 0 },
		"negative window":               func(p *types.Params) { p.RequestTtlBlocks = -1 },
		"huge window":                   func(p *types.Params) { p.TerminalRetentionBlocks = 1 << 40 },
		"zero count":                    func(p *types.Params) { p.MaxDeadlinesPerBlock = 0 },
		"negative fee":                  func(p *types.Params) { p.RequestFee = math.NewInt(-1) },
		"trust level above CORE":        func(p *types.Params) { p.MinRequesterTrustLevel = 5 },
		"claim window at the ttl":       func(p *types.Params) { p.ClaimWindowBlocks = p.RequestTtlBlocks },
		"unreachable at the ttl":        func(p *types.Params) { p.MinUnreachableBlocks = p.RequestTtlBlocks },
		"dispute shorter than settle":   func(p *types.Params) { p.DisputeWindowBlocks = p.SettleWindowBlocks - 1 },
		"retention inside the hold":     func(p *types.Params) { p.TerminalRetentionBlocks = p.FlagHoldBlocks },
		"list cap above its hard bound": func(p *types.Params) { p.MaxBarredPerRequest = 1000 },
		"tiny url cap":                  func(p *types.Params) { p.MaxUrlBytes = 4 },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := types.DefaultParams()
			mut(&p)
			require.Error(t, p.Validate())
		})
	}
	// A zero request fee is accepted.
	p := types.DefaultParams()
	p.RequestFee = math.ZeroInt()
	require.NoError(t, p.Validate())
}

func TestBackendValidateAndFee(t *testing.T) {
	b := types.DefaultArweaveBackend()
	require.NoError(t, b.Validate())
	require.Equal(t, b.MinFee, b.CopyFee(1))
	require.Equal(t, int64(10*(1<<20)), b.CopyFee(1<<20).Int64())

	cases := map[string]func(b *types.Backend){
		"bad name":            func(b *types.Backend) { b.Name = "Arweave" },
		"no service type":     func(b *types.Backend) { b.ServiceType = "" },
		"no kind":             func(b *types.Backend) { b.Kind = types.BackendKind_BACKEND_KIND_UNSPECIFIED },
		"no locator format":   func(b *types.Backend) { b.LocatorFormat = types.LocatorFormat_LOCATOR_FORMAT_UNSPECIFIED },
		"floor above ceiling": func(b *types.Backend) { b.MinPricePerByte = b.MaxPricePerByte.AddRaw(1) },
		"price below floor":   func(b *types.Backend) { b.PricePerByte = math.ZeroInt() },
		"min fee above cap":   func(b *types.Backend) { b.MinFee = b.MaxMinFee.AddRaw(1) },
		"no decline quorum":   func(b *types.Backend) { b.DeclinesToClose = 0 },
		"type listed twice":   func(b *types.Backend) { b.Types = append(b.Types, b.Types[0]) },
		"zero max size":       func(b *types.Backend) { b.Types[0].MaxSize = 0 },
		"bad content type":    func(b *types.Backend) { b.Types[0].ContentType = "Image/PNG" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			b := types.DefaultArweaveBackend()
			mut(&b)
			require.Error(t, b.Validate())
		})
	}
}

func TestRequiredAttestations(t *testing.T) {
	require.Equal(t, uint32(1), types.RequiredAttestations(1, 0, 0)) // no elevated quorum set
	require.Equal(t, uint32(3), types.RequiredAttestations(1, 3, 0)) // NEW
	require.Equal(t, uint32(3), types.RequiredAttestations(1, 3, 1)) // PROVISIONAL
	require.Equal(t, uint32(1), types.RequiredAttestations(1, 3, 2)) // ESTABLISHED
	require.Equal(t, uint32(0), types.RequiredAttestations(0, 0, 4))
}

func TestDefaultGenesisIsValid(t *testing.T) {
	g := types.DefaultGenesis()
	require.NoError(t, g.Validate())
	require.Len(t, g.Backends, 1)
	require.Empty(t, g.ApprovedOperators)
	require.Empty(t, g.ApprovedScanners)
}

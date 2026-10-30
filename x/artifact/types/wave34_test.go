package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestDefaultWave34Params(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, uint32(3), p.MaxNestDepth)
	require.Equal(t, uint32(20), p.MaxNestedTokens)
	require.Equal(t, int64(90*24*3600), p.MaxRentalDuration)
	require.Equal(t, uint32(10), p.MaxCraftRecipesPerClass)
}

func TestHarbergerPolicyValidate(t *testing.T) {
	ok := types.HarbergerPolicy{TaxBpsPerYear: 500, TaxPeriod: types.MinHarbergerPeriod, ForeclosureDuration: types.MaxHarbergerPeriod}
	require.NoError(t, ok.Validate())
	require.NoError(t, types.HarbergerPolicy{}.Validate())
	for name, h := range map[string]types.HarbergerPolicy{
		"fields without a rate": {TaxPeriod: 60},
		"rate above 100%":       {TaxBpsPerYear: types.MaxHarbergerTaxBps + 1, TaxPeriod: types.MinHarbergerPeriod, ForeclosureDuration: types.MinHarbergerPeriod},
		"period too short":      {TaxBpsPerYear: 1, TaxPeriod: types.MinHarbergerPeriod - 1, ForeclosureDuration: types.MinHarbergerPeriod},
		"foreclosure too long":  {TaxBpsPerYear: 1, TaxPeriod: types.MinHarbergerPeriod, ForeclosureDuration: types.MaxHarbergerPeriod + 1},
	} {
		require.ErrorIs(t, h.Validate(), types.ErrInvalidHarberger, name)
	}
}

func TestTaxPerPeriod(t *testing.T) {
	daily := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 86400, ForeclosureDuration: 86400}
	cases := []struct {
		price, want int64
	}{
		{365_000_000, 100_000}, // exact: 10%/yr of 365 SPARK for a day
		{1, 1},                 // a priced token never pays zero
		{0, 0},
		{365_000_001, 100_001}, // rounded up
	}
	for _, tc := range cases {
		require.Equal(t, math.NewInt(tc.want), daily.TaxPerPeriod(math.NewInt(tc.price)), "price %d", tc.price)
	}
	require.True(t, types.HarbergerPolicy{}.TaxPerPeriod(math.NewInt(100)).IsZero())
}

func TestCurrentHarbergerPrice(t *testing.T) {
	h := types.HarbergerPolicy{TaxBpsPerYear: 1000, TaxPeriod: 100, ForeclosureDuration: 1000}
	price := math.NewInt(1_000_000)

	// Priced with a grant: the assessment stands.
	withGrant := types.HarbergerState{Price: price, GrantId: 1, NextDue: 500, GraceUntil: 100}
	got, foreclosed := types.CurrentHarbergerPrice(h, withGrant, 5000)
	require.Equal(t, price, got)
	require.False(t, foreclosed)

	// No grant: foreclosure starts at the end of the grace period.
	noGrant := types.HarbergerState{Price: price, GraceUntil: 100}
	got, foreclosed = types.CurrentHarbergerPrice(h, noGrant, 99)
	require.Equal(t, price, got)
	require.False(t, foreclosed)
	got, foreclosed = types.CurrentHarbergerPrice(h, noGrant, 600)
	require.Equal(t, math.NewInt(500_000), got)
	require.True(t, foreclosed)
	got, _ = types.CurrentHarbergerPrice(h, noGrant, 1100)
	require.True(t, got.IsZero())

	// A recorded foreclosure outranks the grace period.
	missed := types.HarbergerState{Price: price, ForeclosedAt: 200, GraceUntil: 10_000}
	got, foreclosed = types.CurrentHarbergerPrice(h, missed, 450)
	require.Equal(t, math.NewInt(750_000), got)
	require.True(t, foreclosed)

	// An unpriced token is for sale only once foreclosure starts.
	unpriced := types.HarbergerState{Price: math.ZeroInt(), GraceUntil: 100}
	require.False(t, types.HarbergerForSale(unpriced, 50))
	require.True(t, types.HarbergerForSale(unpriced, 100))
	require.True(t, types.HarbergerForSale(withGrant, 0))
}

func TestValidateImpactClaim(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, types.ValidateImpactClaim(types.ImpactClaim{}, p))
	require.NoError(t, types.ValidateImpactClaim(types.ImpactClaim{Scope: "aurora field guide", Contributors: []string{"a", "b"}, WorkStart: 1, WorkEnd: 2}, p))
	many := make([]string, types.MaxImpactContributors+1)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	for name, c := range map[string]types.ImpactClaim{
		"no scope":            {Contributors: []string{"a"}},
		"duplicate":           {Scope: "s", Contributors: []string{"a", "a"}},
		"too many":            {Scope: "s", Contributors: many},
		"period reversed":     {Scope: "s", WorkStart: 5, WorkEnd: 4},
		"negative":            {Scope: "s", WorkStart: -1},
		"scope with data uri": {Scope: "see data:text/html;base64,AAAA"},
	} {
		require.ErrorIs(t, types.ValidateImpactClaim(c, p), types.ErrInvalidClaim, name)
	}
}

func TestBalanceAccrual(t *testing.T) {
	rpu := types.RewardPerUnitDelta(math.NewInt(1_000), 3) // 1000 over 3 units
	b := types.Balance{Amount: 2, Locked: 1, RewardDebt: math.ZeroInt(), Unclaimed: math.NewInt(7)}
	require.Equal(t, math.NewInt(999), b.Accrued(rpu)) // floor; 1 stays as dust
	require.Equal(t, math.NewInt(1006), b.Claimable(rpu))
	require.True(t, types.RewardPerUnitDelta(math.NewInt(5), 0).IsZero())
	b.RewardDebt = rpu
	require.True(t, b.Accrued(rpu).IsZero())
}

func TestContentsHash(t *testing.T) {
	require.Equal(t, "", types.ContentsHash(nil))
	a := []types.ContentsEntry{{ClassID: 2, TokenID: 1, ParentClassID: 1, ParentTokenID: 1, MetadataHash: "aa"},
		{ClassID: 2, TokenID: 2, ParentClassID: 2, ParentTokenID: 1, MetadataHash: "bb"}}
	b := []types.ContentsEntry{a[1], a[0]}
	require.Equal(t, types.ContentsHash(a), types.ContentsHash(b), "order independent")
	require.Len(t, types.ContentsHash(a), 64)
	c := append([]types.ContentsEntry(nil), a...)
	c[1].ParentTokenID = 9 // same tokens, different shape
	require.NotEqual(t, types.ContentsHash(a), types.ContentsHash(c))
	c = append([]types.ContentsEntry(nil), a...)
	c[0].MetadataHash = "cc" // swapped media
	require.NotEqual(t, types.ContentsHash(a), types.ContentsHash(c))
}

func TestFlagsNeither(t *testing.T) {
	f := types.ClassFlags{Transferable: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_NEITHER}
	require.NoError(t, f.Validate(), "nobody burns, so the issuer cannot either: transferable is fine")
	require.False(t, f.HolderMayBurn())
	require.False(t, f.IssuerMayBurn())
	require.True(t, f.NobodyMayBurn())
	require.True(t, f.NeedsConsent())
	require.False(t, types.ClassFlags{}.NeedsConsent())
}

func TestRenderStateV1(t *testing.T) {
	c := types.Class{Id: 3, Supply: 9, Burned: 1}
	tok := types.Token{ClassId: 3, Id: 4, Owner: "zenith", Minter: "aurora", MintedAt: 10, Lock: types.TokenLock_TOKEN_LOCK_NESTED,
		Parent: &types.TokenRef{ClassId: 2, TokenId: 7}, Descendants: 1}
	doc := types.NewRenderStateV1(c, tok, "phoenix", true, 2)
	var round map[string]any
	require.NoError(t, json.Unmarshal([]byte(doc.JSON()), &round))
	require.Equal(t, float64(1), round["v"])
	require.Equal(t, "2/7", round["parent"])
	require.Equal(t, "phoenix", round["user"])
	require.Equal(t, "TOKEN_LOCK_NESTED", round["lock"])
	require.Equal(t, float64(2), round["owner_trust_level"])
	// The v1 field order is frozen: the JSON starts with the version.
	require.True(t, strings.HasPrefix(doc.JSON(), `{"v":1,"owner":"zenith","minter":"aurora"`))
	expr := types.RenderStateExpr(3, 4, "ab", 100, 5, doc.JSON())
	require.True(t, strings.HasPrefix(expr, `RenderState(3, 4, "ab", 100, 5, "{\"v\":1`))
}

func TestRendererStateVersion(t *testing.T) {
	pkg := goodPackage()
	r := types.Renderer{PkgPath: "gno.land/p/demo/aurora", SourceHash: types.RendererSourceHash(pkg.Files)}
	require.NoError(t, types.ValidateRendererPackage(r, pkg))
	r.StateVersion = types.RenderStateV1
	require.ErrorIs(t, types.ValidateRendererPackage(r, pkg), types.ErrInvalidRenderer, "v1 needs RenderState")
	pkg.Funcs = append(pkg.Funcs, types.GnoFuncSig{Name: "RenderState",
		Params: []string{"uint64", "uint64", "string", "int64", "int64", "string"}, Results: []string{"string"}})
	require.NoError(t, types.ValidateRendererPackage(r, pkg))
	r.StateVersion = types.MaxRenderStateVersion + 1
	require.ErrorIs(t, types.ValidateRendererPackage(r, pkg), types.ErrInvalidRenderer)
}

func TestRenderStateV2(t *testing.T) {
	v1 := types.NewRenderStateV1(types.Class{Id: 1}, types.Token{ClassId: 1, Id: 2, Owner: "aurora"}, "", false, 0)
	doc := types.NewRenderStateV2(v1, types.SeasonInfo{Number: 3, Status: "SEASON_STATUS_ACTIVE"}, types.MemberSeason{Level: 5, XP: 900}, nil, "")
	js := doc.JSON()
	require.True(t, strings.HasPrefix(js, `{"v":2,"owner":"aurora"`), "v1's fields first, in v1's order")
	require.Contains(t, js, `"children":[]`, "an empty list, never null")
	require.True(t, strings.HasSuffix(js, `"season_number":3,"season_status":"SEASON_STATUS_ACTIVE","owner_season_level":5,"owner_season_xp":900,"children":[],"contents_hash":""}`))
	require.Equal(t, types.RenderStateV2, types.MaxRenderStateVersion)
}

// AddUnits never wraps and never passes the compiled edition ceiling.
func TestAddUnits(t *testing.T) {
	n, err := types.AddUnits(2, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(5), n)
	n, err = types.AddUnits(types.HardMaxEditionSupply-1, 1)
	require.NoError(t, err)
	require.Equal(t, types.HardMaxEditionSupply, n)
	for _, c := range [][2]uint64{{types.HardMaxEditionSupply, 1}, {1, ^uint64(0)}, {^uint64(0), 2}, {0, types.HardMaxEditionSupply + 1}} {
		_, err = types.AddUnits(c[0], c[1])
		require.ErrorIs(t, err, types.ErrSupplyExceeded, "%d + %d", c[0], c[1])
	}
}

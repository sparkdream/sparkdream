package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

// setAllowlist is a small helper that sets the authorized_grant_creators
// param to the given list, leaving every other param at default.
func setAllowlist(t *testing.T, f *fixture, addrs ...string) {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.AuthorizedGrantCreators = addrs
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
}

func TestCreateGrantOnBehalfOf_BypassDisabledWhenAllowlistEmpty(t *testing.T) {
	f := initFixture(t)
	// Clear the genesis-default allowlist (seeded with the commons
	// module address by M3) so we exercise the "bypass disabled"
	// path explicitly.
	setAllowlist(t, f)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, granter, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: time.Now().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authorized_grant_creators allowlist")
}

func TestCreateGrantOnBehalfOf_CallerNotInAllowlist(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	authorized := authtypes.NewModuleAddress("commons").String()
	imposter := testAddr("imposter_module", f.addressCodec)

	setAllowlist(t, f, authorized)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, imposter, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in allowlist")
}

func TestCreateGrantOnBehalfOf_RecurringPullHappyPath(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Note:      "council recurring spend imported via bypass",
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)
	require.Greater(t, id, uint64(0))

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_RECURRING_PULL, g.Type)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	require.Equal(t, granter, g.Granter)
	require.Equal(t, grantee, g.Grantee)
	require.Equal(t, "council recurring spend imported via bypass", g.Note)
}

func TestCreateGrantOnBehalfOf_PayloadValidationStillApplies(t *testing.T) {
	// Verifies a buggy caller cannot smuggle a DREAM-denominated grant
	// through the bypass — payload validation runs on the same code
	// path as MsgCreateGrant.
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   testAddr("module_acct", f.addressCodec),
		Grantee:   testAddr("recipient", f.addressCodec),
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("udream", 1_000_000), // forbidden
				PeriodSeconds:   86_400,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dream denom is forbidden")
}

func TestCreateGrantOnBehalfOf_NilMsg(t *testing.T) {
	f := initFixture(t)
	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}

func TestRevokeGrantInternal_BypassDisabledWhenAllowlistEmpty(t *testing.T) {
	f := initFixture(t)
	// Clear the genesis-default allowlist (M3 seeds it with the
	// commons module address) so we exercise the "bypass disabled"
	// path explicitly.
	setAllowlist(t, f)

	caller := authtypes.NewModuleAddress("commons").String()

	_, err := f.keeper.RevokeGrantInternal(f.ctx, caller, 42)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authorized_grant_creators allowlist")
}

func TestRevokeGrantInternal_HappyPath(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	// Create via the bypass.
	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// Revoke via the bypass.
	refund, err := f.keeper.RevokeGrantInternal(f.ctx, caller, id)
	require.NoError(t, err)
	require.True(t, refund.IsZero()) // non-oneshot: zero refund

	// Grant is gone.
	_, err = f.keeper.Grants.Get(f.ctx, id)
	require.Error(t, err)

	// Active counter decremented.
	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Equal(t, uint32(0), count)
}

func TestRevokeGrantInternal_CallerNotAuthorized(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	authorized := authtypes.NewModuleAddress("commons").String()
	imposter := authtypes.NewModuleAddress("federation").String()
	setAllowlist(t, f, authorized)

	// Create via authorized caller.
	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, authorized, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// Imposter tries to revoke.
	_, err = f.keeper.RevokeGrantInternal(f.ctx, imposter, id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in allowlist")

	// Grant still exists.
	_, err = f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
}

func TestParamsValidate_AuthorizedGrantCreatorsInvalid(t *testing.T) {
	// Invalid bech32 entries are rejected at Params.Validate time.
	params := types.DefaultParams()
	params.AuthorizedGrantCreators = []string{"not_a_bech32_address"}
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid address")
}

func TestParamsValidate_AuthorizedGrantCreatorsDuplicates(t *testing.T) {
	addr := authtypes.NewModuleAddress("commons").String()
	params := types.DefaultParams()
	params.AuthorizedGrantCreators = []string{addr, addr}
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}

// ---------------------------------------------------------------------------
// M2 — read helpers: GetGrant, ListGrantsByGranter, ListGrantsByGrantee.
// ---------------------------------------------------------------------------

func TestGetGrant_NotFoundMapsToErrGrantNotFound(t *testing.T) {
	f := initFixture(t)
	_, err := f.keeper.GetGrant(f.ctx, 99999)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrGrantNotFound)
}

func TestGetGrant_RoundTrip(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	g, err := f.keeper.GetGrant(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, g.Id)
	require.Equal(t, granter, g.Granter)
	require.Equal(t, grantee, g.Grantee)
	require.Equal(t, types.GrantType_GRANT_TYPE_RECURRING_PULL, g.Type)
}

func TestListGrantsByGranter_FilteringAndOrdering(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	granteeA := testAddr("recipientA", f.addressCodec)
	granteeB := testAddr("recipientB", f.addressCodec)

	// Two RecurringPull grants by the same granter, to different grantees.
	idA, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   granteeA,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)
	idB, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   granteeB,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 2_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	// Unfiltered: both grants.
	grants, err := f.keeper.ListGrantsByGranter(f.ctx, granter, types.GrantType_GRANT_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	require.Equal(t, idA, grants[0].Id)
	require.Equal(t, idB, grants[1].Id)

	// Filtered to RECURRING_PULL: still both.
	grants, err = f.keeper.ListGrantsByGranter(f.ctx, granter, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Len(t, grants, 2)

	// Filtered to a different type: zero.
	grants, err = f.keeper.ListGrantsByGranter(f.ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	require.NoError(t, err)
	require.Empty(t, grants)
}

func TestListGrantsByGrantee_ReturnsAllForGrantee(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granterA := testAddr("module_acctA", f.addressCodec)
	granterB := testAddr("module_acctB", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	// Two grants to the same grantee from different granters.
	_, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granterA,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)
	_, err = f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granterB,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	grants, err := f.keeper.ListGrantsByGrantee(f.ctx, grantee, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	// Verify the result set covers both granters.
	gset := map[string]bool{grants[0].Granter: true, grants[1].Granter: true}
	require.True(t, gset[granterA])
	require.True(t, gset[granterB])
}

// ---------------------------------------------------------------------------
// M2 — DeclineGrantInternal: allowlist gate, grantee mismatch, happy path.
// ---------------------------------------------------------------------------

func TestDeclineGrantInternal_BypassDisabledWhenAllowlistEmpty(t *testing.T) {
	f := initFixture(t)
	setAllowlist(t, f) // explicit clear; M3 seeds commons by default
	caller := authtypes.NewModuleAddress("commons").String()
	_, err := f.keeper.DeclineGrantInternal(f.ctx, caller, 1, "ignored")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrBypassDisabled)
}

func TestDeclineGrantInternal_CallerNotAuthorized(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	authorized := authtypes.NewModuleAddress("commons").String()
	imposter := authtypes.NewModuleAddress("federation").String()
	setAllowlist(t, f, authorized)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, authorized, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	_, err = f.keeper.DeclineGrantInternal(f.ctx, imposter, id, grantee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in allowlist")

	_, err = f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err) // grant survives
}

func TestDeclineGrantInternal_GranteeMismatch(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	imposter := testAddr("imposter", f.addressCodec)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	_, err = f.keeper.DeclineGrantInternal(f.ctx, caller, id, imposter)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrDeclineUnauthorized)

	_, err = f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err) // grant survives
}

func TestDeclineGrantInternal_HappyPath(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	refund, err := f.keeper.DeclineGrantInternal(f.ctx, caller, id, grantee)
	require.NoError(t, err)
	require.True(t, refund.IsZero()) // non-oneshot: zero refund

	_, err = f.keeper.Grants.Get(f.ctx, id)
	require.Error(t, err) // grant deleted

	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Equal(t, uint32(0), count)
}

// ---------------------------------------------------------------------------
// M2 — ClaimRecurringPullForGrantee: allowlist gate, parity with msg-server.
// ---------------------------------------------------------------------------

func TestClaimRecurringPullForGrantee_BypassDisabledWhenAllowlistEmpty(t *testing.T) {
	f := initFixture(t)
	setAllowlist(t, f) // explicit clear; M3 seeds commons by default
	caller := authtypes.NewModuleAddress("commons").String()
	_, err := f.keeper.ClaimRecurringPullForGrantee(f.ctx, caller, 1, "ignored")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrBypassDisabled)
}

func TestClaimRecurringPullForGrantee_CallerNotAuthorized(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	authorized := authtypes.NewModuleAddress("commons").String()
	imposter := authtypes.NewModuleAddress("federation").String()
	setAllowlist(t, f, authorized)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, authorized, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.NoError(t, err)

	_, err = f.keeper.ClaimRecurringPullForGrantee(f.ctx, imposter, id, grantee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in allowlist")
}

func TestClaimRecurringPullForGrantee_HappyPath(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	period := int64(86_400)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(30 * 24 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: amount,
				PeriodSeconds:   period,
			},
		},
	})
	require.NoError(t, err)

	// Stub bank to succeed.
	var bankCalls int
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		bankCalls++
		return nil
	}

	// Advance past first eligible window.
	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	resp, err := f.keeper.ClaimRecurringPullForGrantee(futureCtx, caller, id, grantee)
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.ClaimNumber)
	require.Equal(t, 1, bankCalls)
}

// ---------------------------------------------------------------------------
// M3 — genesis default for AuthorizedGrantCreators.
// ---------------------------------------------------------------------------

// TestDefaultParams_SeedsCommonsModuleInAllowlist confirms the M3
// genesis bootstrap: DefaultParams() ships with the commons module
// address pre-seeded in authorized_grant_creators so the x/commons
// RecurringSpend wrapper can route through the unified registry from
// block 0 without a post-launch gov proposal.
func TestDefaultParams_SeedsCommonsModuleInAllowlist(t *testing.T) {
	params := types.DefaultParams()
	require.NotEmpty(t, params.AuthorizedGrantCreators,
		"M3 requires DefaultParams to seed the bypass allowlist")
	expected := authtypes.NewModuleAddress("commons").String()
	require.Contains(t, params.AuthorizedGrantCreators, expected,
		"commons module address must be in the genesis-default allowlist")
}

// TestDefaultParams_ValidateIncludesSeededAllowlist confirms the
// genesis default passes Params.Validate (no broken default
// regression).
func TestDefaultParams_ValidateIncludesSeededAllowlist(t *testing.T) {
	require.NoError(t, types.DefaultParams().Validate())
}

func TestClaimRecurringPullForGrantee_GranteeMismatchRejected(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	caller := authtypes.NewModuleAddress("commons").String()
	setAllowlist(t, f, caller)

	granter := testAddr("module_acct", f.addressCodec)
	grantee := testAddr("recipient", f.addressCodec)
	imposter := testAddr("imposter", f.addressCodec)
	period := int64(86_400)

	id, err := f.keeper.CreateGrantOnBehalfOf(f.ctx, caller, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(30 * 24 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   period,
			},
		},
	})
	require.NoError(t, err)

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err = f.keeper.ClaimRecurringPullForGrantee(futureCtx, caller, id, imposter)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRecurringPullUnauthorized)
}

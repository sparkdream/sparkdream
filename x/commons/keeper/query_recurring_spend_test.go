package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// queryFixture wires a commons keeper + mockSessionKeeper + a
// registered council, returning the queryServer for the projection
// tests below.
type queryFixture struct {
	t        *testing.T
	k        keeper.Keeper
	ctx      sdk.Context
	mock     *mockSessionKeeper
	q        types.QueryServer
	council  sdk.AccAddress
	council2 sdk.AccAddress
}

func setupQueryFixture(t *testing.T) *queryFixture {
	t.Helper()
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	mock := newMockSessionKeeper()
	k.SetSessionKeeper(mock)

	return &queryFixture{
		t:        t,
		k:        k,
		ctx:      ctx,
		mock:     mock,
		q:        keeper.NewQueryServerImpl(k),
		council:  sdk.AccAddress([]byte("query_council_______")),
		council2: sdk.AccAddress([]byte("query_council2______")),
	}
}

// seedGrant inserts a synthetic RECURRING_PULL grant into the mock
// session keeper. Returns the grant id.
func (f *queryFixture) seedGrant(t *testing.T, granter, grantee string, id uint64, statusFlag sessiontypes.GrantStatus) uint64 {
	t.Helper()
	grant := sessiontypes.Grant{
		Id:        id,
		Type:      sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL,
		Granter:   granter,
		Grantee:   grantee,
		Status:    statusFlag,
		ExpiresAt: f.ctx.BlockTime().Add(30 * 24 * time.Hour),
		Note:      "test",
		Payload: &sessiontypes.Grant_RecurringPull{
			RecurringPull: &sessiontypes.RecurringPullPayload{
				AmountPerPeriod:   sdk.NewCoin("uspark", math.NewInt(100)),
				PeriodSeconds:     86_400,
				StartTime:         f.ctx.BlockTime().Unix(),
				LastClaimAdvance:  f.ctx.BlockTime().Unix(),
				ClaimsMade:        0,
				MaxPerEpochUspark: "10000",
			},
		},
	}
	f.mock.grants[id] = grant
	return id
}

// ---------------------------------------------------------------------------
// GetRecurringSpend
// ---------------------------------------------------------------------------

func TestQueryGetRecurringSpend_Nil(t *testing.T) {
	f := setupQueryFixture(t)
	_, err := f.q.GetRecurringSpend(f.ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryGetRecurringSpend_NotFound(t *testing.T) {
	f := setupQueryFixture(t)
	_, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: 999})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryGetRecurringSpend_FoundProjectsCorrectly(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	id := f.seedGrant(t, f.council.String(), recipient, 7,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)

	resp, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, id, resp.RecurringSpend.Id)
	require.Equal(t, f.council.String(), resp.RecurringSpend.Authority)
	require.Equal(t, recipient, resp.RecurringSpend.Recipient)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE, resp.RecurringSpend.Status)
	require.Len(t, resp.RecurringSpend.AmountPerPeriod, 1)
	require.Equal(t, "uspark", resp.RecurringSpend.AmountPerPeriod[0].Denom)
	require.Equal(t, int64(100), resp.RecurringSpend.AmountPerPeriod[0].Amount.Int64())
	require.Equal(t, int64(86_400), resp.RecurringSpend.PeriodSeconds)
	// created_via_proposal_id is the dead-field zero.
	require.Equal(t, uint64(0), resp.RecurringSpend.CreatedViaProposalId)
}

// TestQueryGetRecurringSpend_CancelledNotFound is the §1/§8 deliberate
// semantic break: a cancelled grant is DELETED from session, so
// post-cancel the query returns NotFound (audit trail in the
// grant_revoked event). Tested by simulating "cancel deleted the
// grant" in the mock.
func TestQueryGetRecurringSpend_CancelledNotFound(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	id := f.seedGrant(t, f.council.String(), recipient, 8,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)

	// Simulate cancel: delete the grant from session storage.
	delete(f.mock.grants, id)

	_, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: id})
	require.Equal(t, codes.NotFound, status.Code(err),
		"cancelled/declined grants return NotFound — audit in events (§1)")
}

func TestQueryGetRecurringSpend_CompletedStillQueryable(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	id := f.seedGrant(t, f.council.String(), recipient, 9,
		sessiontypes.GrantStatus_GRANT_STATUS_COMPLETED)

	resp, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED, resp.RecurringSpend.Status)
}

func TestQueryGetRecurringSpend_PausedProjectsAsActive(t *testing.T) {
	// PAUSED_INSUFFICIENT_FUNDS isn't surfaced through the legacy
	// commons enum; projection collapses it to ACTIVE so the response
	// stays well-formed. (Note that PAUSED itself isn't durably
	// observable in production today — pre-existing session concern;
	// see §4.)
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	id := f.seedGrant(t, f.council.String(), recipient, 10,
		sessiontypes.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS)

	resp, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE, resp.RecurringSpend.Status)
}

// TestQueryGetRecurringSpend_WrongGrantType confirms a non-recurring-pull
// grant id (e.g. a SessionKey id collision in the future) returns
// NotFound rather than panicking on the missing payload field.
func TestQueryGetRecurringSpend_WrongGrantType(t *testing.T) {
	f := setupQueryFixture(t)
	f.mock.grants[42] = sessiontypes.Grant{
		Id:      42,
		Type:    sessiontypes.GrantType_GRANT_TYPE_SESSION_KEY,
		Granter: f.council.String(),
		Grantee: "x",
		Status:  sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
	}

	_, err := f.q.GetRecurringSpend(f.ctx, &types.QueryGetRecurringSpendRequest{Id: 42})
	require.Equal(t, codes.NotFound, status.Code(err))
}

// ---------------------------------------------------------------------------
// ListRecurringSpends
// ---------------------------------------------------------------------------

func TestQueryListRecurringSpends_RejectsBothFilters(t *testing.T) {
	f := setupQueryFixture(t)
	_, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Authority: "alice",
		Recipient: "bob",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestQueryListRecurringSpends_RejectsNoFilter is the §1/§8 semantic
// break: cross-granter pagination is intentionally not supported
// post-migration (no efficient session iterator). At-least-one-filter
// is required.
func TestQueryListRecurringSpends_RejectsNoFilter(t *testing.T) {
	f := setupQueryFixture(t)
	_, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryListRecurringSpends_ByAuthority(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	f.seedGrant(t, f.council.String(), recipient, 1,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)
	f.seedGrant(t, f.council.String(), recipient, 2,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)
	f.seedGrant(t, f.council2.String(), recipient, 3,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)

	resp, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Authority: f.council.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.RecurringSpends, 2)
	for _, s := range resp.RecurringSpends {
		require.Equal(t, f.council.String(), s.Authority)
	}
}

func TestQueryListRecurringSpends_ByRecipient(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	f.seedGrant(t, f.council.String(), recipient, 1,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)
	f.seedGrant(t, f.council2.String(), recipient, 2,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)
	f.seedGrant(t, f.council.String(), "other", 3,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)

	resp, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Recipient: recipient,
	})
	require.NoError(t, err)
	require.Len(t, resp.RecurringSpends, 2)
	for _, s := range resp.RecurringSpends {
		require.Equal(t, recipient, s.Recipient)
	}
}

func TestQueryListRecurringSpends_UnknownAuthorityEmpty(t *testing.T) {
	f := setupQueryFixture(t)
	resp, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Authority: sdk.AccAddress([]byte("nobody______________")).String(),
	})
	require.NoError(t, err)
	require.Empty(t, resp.RecurringSpends)
}

func TestQueryListRecurringSpends_PaginationOffsetLimit(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	for i := uint64(1); i <= 5; i++ {
		f.seedGrant(t, f.council.String(), recipient, i,
			sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)
	}

	resp, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Authority:  f.council.String(),
		Pagination: &query.PageRequest{Offset: 1, Limit: 2, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, resp.RecurringSpends, 2)
	require.Equal(t, uint64(5), resp.Pagination.Total,
		"CountTotal=true reflects the full pre-paginated slice length")
}

func TestQueryListRecurringSpends_OffsetPastEndReturnsEmpty(t *testing.T) {
	f := setupQueryFixture(t)
	recipient := sdk.AccAddress([]byte("recipient_q_________")).String()
	f.seedGrant(t, f.council.String(), recipient, 1,
		sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE)

	resp, err := f.q.ListRecurringSpends(f.ctx, &types.QueryListRecurringSpendsRequest{
		Authority:  f.council.String(),
		Pagination: &query.PageRequest{Offset: 10, Limit: 5},
	})
	require.NoError(t, err)
	require.Empty(t, resp.RecurringSpends)
}

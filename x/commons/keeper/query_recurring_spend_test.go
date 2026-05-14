package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// seedRecurringSpends drops four schedules into the store covering both
// authorities and a single recipient, so the index-backed list queries
// have a non-trivial mix to walk.
func seedRecurringSpends(t *testing.T, k keeper.Keeper, ctx sdk.Context) (string, string, string) {
	t.Helper()
	authorityA := sdk.AccAddress([]byte("query_council_A_____")).String()
	authorityB := sdk.AccAddress([]byte("query_council_B_____")).String()
	recipient := sdk.AccAddress([]byte("query_recipient_____")).String()

	amt := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100)))
	schedules := []types.RecurringSpend{
		{Id: 1, Authority: authorityA, Recipient: recipient, AmountPerPeriod: amt, PeriodSeconds: 86400, Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE},
		{Id: 2, Authority: authorityA, Recipient: recipient, AmountPerPeriod: amt, PeriodSeconds: 86400, Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE},
		{Id: 3, Authority: authorityB, Recipient: recipient, AmountPerPeriod: amt, PeriodSeconds: 86400, Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE},
		{Id: 4, Authority: authorityA, Recipient: recipient, AmountPerPeriod: amt, PeriodSeconds: 86400, Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_CANCELED},
	}
	// Use the keeper's own setter so secondary indexes get populated.
	for _, s := range schedules {
		require.NoError(t, k.RecurringSpends.Set(ctx, s.Id, s))
		// Direct collections insert (no helper); test seeds the indexes manually
		// to mirror what setRecurringSpend does, since that method is unexported.
		require.NoError(t, k.RecurringSpendsByAuthority.Set(ctx, collections.Join(s.Authority, s.Id)))
		require.NoError(t, k.RecurringSpendsByRecipient.Set(ctx, collections.Join(s.Recipient, s.Id)))
	}
	return authorityA, authorityB, recipient
}

func TestQueryGetRecurringSpend_Nil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.GetRecurringSpend(ctx, nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryGetRecurringSpend_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.GetRecurringSpend(ctx, &types.QueryGetRecurringSpendRequest{Id: 999})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryGetRecurringSpend_Found(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)
	authA, _, _ := seedRecurringSpends(t, k, ctx)

	resp, err := q.GetRecurringSpend(ctx, &types.QueryGetRecurringSpendRequest{Id: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.RecurringSpend.Id)
	require.Equal(t, authA, resp.RecurringSpend.Authority)
}

func TestQueryListRecurringSpends_RejectsBothFilters(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)

	_, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{
		Authority: "alice",
		Recipient: "bob",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"providing both authority and recipient must error — neither index obviously wins")
}

func TestQueryListRecurringSpends_NoFilter(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)
	seedRecurringSpends(t, k, ctx)

	resp, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.RecurringSpends, 4, "no filter → every record returned, regardless of status")
}

func TestQueryListRecurringSpends_ByAuthority(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)
	authA, authB, _ := seedRecurringSpends(t, k, ctx)

	respA, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{Authority: authA})
	require.NoError(t, err)
	// authA owns ids 1, 2, 4 (ACTIVE + ACTIVE + CANCELED) — the index covers ALL statuses.
	require.Len(t, respA.RecurringSpends, 3)
	for _, s := range respA.RecurringSpends {
		require.Equal(t, authA, s.Authority)
	}

	respB, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{Authority: authB})
	require.NoError(t, err)
	require.Len(t, respB.RecurringSpends, 1)
	require.Equal(t, uint64(3), respB.RecurringSpends[0].Id)
}

func TestQueryListRecurringSpends_ByRecipient(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)
	_, _, recipient := seedRecurringSpends(t, k, ctx)

	resp, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{Recipient: recipient})
	require.NoError(t, err)
	require.Len(t, resp.RecurringSpends, 4)
}

func TestQueryListRecurringSpends_UnknownAuthority(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	q := keeper.NewQueryServerImpl(k)
	seedRecurringSpends(t, k, ctx)

	resp, err := q.ListRecurringSpends(ctx, &types.QueryListRecurringSpendsRequest{
		Authority: sdk.AccAddress([]byte("nobody_______________")).String(),
	})
	require.NoError(t, err)
	require.Empty(t, resp.RecurringSpends)
}

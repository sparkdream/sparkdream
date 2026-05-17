package keeper_test

import (
	"sort"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:               types.DefaultParams(),
		PolicyPermissionsMap: []types.PolicyPermissions{{PolicyAddress: "0"}, {PolicyAddress: "1"}}, GroupMap: []types.Group{{Index: "0"}, {Index: "1"}}}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.PolicyPermissionsMap, got.PolicyPermissionsMap)
	require.EqualExportedValues(t, genesisState.GroupMap, got.GroupMap)

}

// TestGenesis_RecurringSpendRoundTrip verifies:
//   - All RecurringSpend records survive InitGenesis → ExportGenesis with
//     value-equal contents and identical IDs.
//   - The next-ID sequence is restored so subsequent ScheduleRecurringSpend
//     calls don't reuse imported IDs.
//   - ActiveRecurringSpendCount is RECOMPUTED from status rather than
//     trusted from the export — only ACTIVE entries count, and terminal
//     statuses (CANCELED/COMPLETED/DECLINED) do not inflate the cap counter.
func TestGenesis_RecurringSpendRoundTrip(t *testing.T) {
	f := initFixture(t)

	authority := sdk.AccAddress([]byte("genesis_council_____")).String()
	recipient := sdk.AccAddress([]byte("genesis_recipient___")).String()
	otherAuth := sdk.AccAddress([]byte("genesis_other_______")).String()

	amount := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100)))
	gen := types.GenesisState{
		Params: types.DefaultParams(),
		RecurringSpends: []types.RecurringSpend{
			{
				Id: 1, Authority: authority, Recipient: recipient,
				AmountPerPeriod: amount, PeriodSeconds: 86400,
				StartTime: 1_000_000, EndTime: 2_000_000,
				LastClaimAdvance: 1_000_000,
				Status:           types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE,
				Note:             "active a",
			},
			{
				Id: 2, Authority: authority, Recipient: recipient,
				AmountPerPeriod: amount, PeriodSeconds: 86400,
				StartTime: 1_000_000, EndTime: 2_000_000,
				LastClaimAdvance: 1_500_000, ClaimsMade: 5,
				Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE,
				Note:   "active b",
			},
			{
				Id: 3, Authority: authority, Recipient: recipient,
				AmountPerPeriod: amount, PeriodSeconds: 86400,
				StartTime: 1_000_000, EndTime: 2_000_000,
				Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_CANCELED,
				Note:   "terminal — should NOT count toward active cap",
			},
			{
				Id: 4, Authority: otherAuth, Recipient: recipient,
				AmountPerPeriod: amount, PeriodSeconds: 86400,
				StartTime: 1_000_000, EndTime: 2_000_000,
				Status: types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE,
				Note:   "other authority",
			},
		},
		NextRecurringSpendId: 5,
	}

	require.NoError(t, f.keeper.InitGenesis(f.ctx, gen))

	// Active count must reflect only ACTIVE statuses, regrouped per authority.
	cntAuthority, err := f.keeper.ActiveRecurringSpendCount.Get(f.ctx, authority)
	require.NoError(t, err)
	require.Equal(t, uint32(2), cntAuthority, "two ACTIVE schedules for 'authority' — CANCELED must not inflate count")

	cntOther, err := f.keeper.ActiveRecurringSpendCount.Get(f.ctx, otherAuth)
	require.NoError(t, err)
	require.Equal(t, uint32(1), cntOther)

	// Sequence must skip the imported max id.
	nextID, err := f.keeper.RecurringSpendSeq.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(5), nextID)

	// Export and confirm every record round-trips.
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	sortByID := func(rs []types.RecurringSpend) {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Id < rs[j].Id })
	}
	sortByID(gen.RecurringSpends)
	sortByID(got.RecurringSpends)
	require.EqualExportedValues(t, gen.RecurringSpends, got.RecurringSpends)
	require.Equal(t, uint64(5), got.NextRecurringSpendId)

	// Indexes must have been re-built — verify both by-authority and by-recipient.
	authIDs, err := f.keeper.ListRecurringSpendsByAuthority(f.ctx, authority)
	require.NoError(t, err)
	require.Len(t, authIDs, 3, "expected 3 schedules (ids 1,2,3) under authority — index covers ALL statuses, not just active")

	recipIDs, err := f.keeper.ListRecurringSpendsByRecipient(f.ctx, recipient)
	require.NoError(t, err)
	require.Len(t, recipIDs, 4)
}

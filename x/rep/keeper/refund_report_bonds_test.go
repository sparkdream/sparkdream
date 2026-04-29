package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// TestRefundReportBonds_PerSignerAmounts ensures the refund path uses the
// recorded per-signer amount instead of an averaged share. Regression test
// for REP-S2-5 (cross-signer bond theft via averaged refund).
func TestRefundReportBonds_PerSignerAmounts(t *testing.T) {
	f := initFixture(t)
	authorizeCouncil(f)
	ms := keeper.NewMsgServerImpl(f.keeper)

	authority := sdk.AccAddress([]byte("auth_perlock____"))
	authorityStr, err := f.addressCodec.BytesToString(authority)
	require.NoError(t, err)

	// Light reporter: locked 100 DREAM (e.g., bond cap on initial report).
	light := sdk.AccAddress([]byte("light_reporter__"))
	lightStr, err := f.addressCodec.BytesToString(light)
	require.NoError(t, err)
	require.NoError(t, f.keeper.Member.Set(f.ctx, light.String(), types.Member{
		Address:        light.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   keeper.PtrInt(math.NewInt(0)),
		StakedDream:    keeper.PtrInt(math.NewInt(100)),
		LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))

	// Heavy cosigner: locked 9000 DREAM.
	heavy := sdk.AccAddress([]byte("heavy_cosigner__"))
	heavyStr, err := f.addressCodec.BytesToString(heavy)
	require.NoError(t, err)
	require.NoError(t, f.keeper.Member.Set(f.ctx, heavy.String(), types.Member{
		Address:        heavy.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   keeper.PtrInt(math.NewInt(0)),
		StakedDream:    keeper.PtrInt(math.NewInt(9000)),
		LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))

	member := sdk.AccAddress([]byte("subj_perlock____"))
	memberStr, err := f.addressCodec.BytesToString(member)
	require.NoError(t, err)

	// Report with two signers and per-signer bond entries (light=100, heavy=9000),
	// total_bond=9100. Averaged refund would give each (9100/2)=4550 — stealing
	// 4450 from heavy and over-paying light.
	require.NoError(t, f.keeper.MemberReport.Set(f.ctx, memberStr, types.MemberReport{
		Member:    memberStr,
		Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		Reporters: []string{lightStr, heavyStr},
		ReporterBonds: []*types.ReporterBondEntry{
			{Address: lightStr, Amount: "100"},
			{Address: heavyStr, Amount: "9000"},
		},
		TotalBond: "9100",
	}))

	_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{
		Creator: authorityStr,
		Member:  memberStr,
		Action:  uint64(types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED),
		Reason:  "dismissed",
	})
	require.NoError(t, err)

	// Light refunded exactly 100 DREAM, leaving 0 staked.
	lightAfter, err := f.keeper.Member.Get(f.ctx, light.String())
	require.NoError(t, err)
	require.Equal(t, int64(0), lightAfter.StakedDream.Int64(), "light reporter should have exactly 100 unlocked, not averaged share")

	// Heavy refunded exactly 9000 DREAM, leaving 0 staked.
	heavyAfter, err := f.keeper.Member.Get(f.ctx, heavy.String())
	require.NoError(t, err)
	require.Equal(t, int64(0), heavyAfter.StakedDream.Int64(), "heavy cosigner should have exactly 9000 unlocked, not averaged share")
}

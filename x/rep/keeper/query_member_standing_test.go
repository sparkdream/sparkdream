package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryMemberStanding(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.MemberStanding(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing member", func(t *testing.T) {
		_, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: ""})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("member with no warnings, no report, no reputation", func(t *testing.T) {
		// Member must be a valid bech32 address for the trust-tier lookup path;
		// if the decode fails the query returns trust_tier=0 but still succeeds.
		addr := sdk.AccAddress([]byte("standing_clean__"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:        addr.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(math.ZeroInt()),
			StakedDream:    keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
			TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
		}))
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)

		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: addrStr})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.WarningCount)
		require.False(t, resp.ActiveReport)
		require.Equal(t, uint64(0), resp.TrustTier)
	})

	t.Run("member with warnings and active report", func(t *testing.T) {
		addr := sdk.AccAddress([]byte("standing_trouble"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:          addr.String(),
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:     keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "250.0"}, // total 250 => tier 3
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		}))
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)

		// Add two warnings.
		require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, 1, types.MemberWarning{
			Id: 1, Member: addrStr, Reason: "first", WarningNumber: 1,
		}))
		require.NoError(t, f.keeper.MemberWarning.Set(f.ctx, 2, types.MemberWarning{
			Id: 2, Member: addrStr, Reason: "second", WarningNumber: 2,
		}))
		// Active (pending) report.
		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, addrStr, types.MemberReport{
			Member: addrStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		}))

		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: addrStr})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.WarningCount)
		require.True(t, resp.ActiveReport)
		require.Equal(t, uint64(3), resp.TrustTier)
	})

	t.Run("resolved report is not active", func(t *testing.T) {
		addr := sdk.AccAddress([]byte("standing_done___"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:        addr.String(),
			Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   keeper.PtrInt(math.ZeroInt()),
			StakedDream:    keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
			TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
		}))
		addrStr, err := f.addressCodec.BytesToString(addr)
		require.NoError(t, err)

		require.NoError(t, f.keeper.MemberReport.Set(f.ctx, addrStr, types.MemberReport{
			Member: addrStr,
			Status: types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
		}))

		resp, err := qs.MemberStanding(f.ctx, &types.QueryMemberStandingRequest{Member: addrStr})
		require.NoError(t, err)
		require.False(t, resp.ActiveReport)
	})
}

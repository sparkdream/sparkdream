package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createMemberWithTrustLevel(k keeper.Keeper, ctx context.Context, address string, trustLevel types.TrustLevel) {
	member := types.Member{
		Address:            address,
		DreamBalance:       PtrInt(math.NewInt(1000)),
		StakedDream:        PtrInt(math.NewInt(500)),
		LifetimeEarned:     PtrInt(math.NewInt(10000)),
		LifetimeBurned:     PtrInt(math.NewInt(100)),
		TrustLevel:         trustLevel,
		ReputationScores:   make(map[string]string),
		LifetimeReputation: make(map[string]string),
	}
	_ = k.Member.Set(ctx, address, member)
}

func memberAddresses(members []types.Member) []string {
	addrs := make([]string, len(members))
	for i, m := range members {
		addrs[i] = m.Address
	}
	return addrs
}

func TestMembersByTrustLevel(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*fixture)
		trustLevel    uint64
		wantAddresses []string
		wantErr       error
	}{
		{
			name: "ReturnsAllMembersForTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "member1", types.TrustLevel_TRUST_LEVEL_NEW)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member2", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member3", types.TrustLevel_TRUST_LEVEL_NEW)
			},
			trustLevel:    uint64(types.TrustLevel_TRUST_LEVEL_NEW),
			wantAddresses: []string{"member1", "member3"},
		},
		{
			name: "EmptyResponseWhenNoMembersForTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "member1", types.TrustLevel_TRUST_LEVEL_NEW)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member2", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
			},
			trustLevel:    uint64(types.TrustLevel_TRUST_LEVEL_CORE),
			wantAddresses: []string{},
		},
		{
			name:          "EmptyResponseWhenNoMembersExist",
			setup:         func(f *fixture) {},
			trustLevel:    uint64(types.TrustLevel_TRUST_LEVEL_NEW),
			wantAddresses: []string{},
		},
		{
			name: "ReturnsAllMembersWithAdminTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "admin1", types.TrustLevel_TRUST_LEVEL_CORE)
				createMemberWithTrustLevel(f.keeper, f.ctx, "admin2", types.TrustLevel_TRUST_LEVEL_CORE)
			},
			trustLevel:    uint64(types.TrustLevel_TRUST_LEVEL_CORE),
			wantAddresses: []string{"admin1", "admin2"},
		},
		{
			name:       "InvalidRequestNil",
			setup:      func(f *fixture) {},
			trustLevel: 0,
			wantErr:    status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryMembersByTrustLevelRequest
			if tc.wantErr == nil {
				req = &types.QueryMembersByTrustLevelRequest{TrustLevel: tc.trustLevel}
			}

			response, err := qs.MembersByTrustLevel(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			require.ElementsMatch(t, tc.wantAddresses, memberAddresses(response.Members))
			// Every returned member must actually carry the requested trust level.
			for _, m := range response.Members {
				require.Equal(t, tc.trustLevel, uint64(m.TrustLevel))
			}
		})
	}
}

func TestMembersByTrustLevel_MultipleMembers(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create multiple members with the same trust level
	createMemberWithTrustLevel(f.keeper, f.ctx, "contributor1", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	createMemberWithTrustLevel(f.keeper, f.ctx, "contributor2", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	createMemberWithTrustLevel(f.keeper, f.ctx, "contributor3", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	// A member at a different level must be excluded.
	createMemberWithTrustLevel(f.keeper, f.ctx, "newcomer", types.TrustLevel_TRUST_LEVEL_NEW)

	response, err := qs.MembersByTrustLevel(f.ctx, &types.QueryMembersByTrustLevelRequest{
		TrustLevel: uint64(types.TrustLevel_TRUST_LEVEL_ESTABLISHED),
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.ElementsMatch(t,
		[]string{"contributor1", "contributor2", "contributor3"},
		memberAddresses(response.Members),
	)
}

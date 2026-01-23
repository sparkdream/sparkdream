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

func TestMembersByTrustLevel(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*fixture)
		trustLevel   uint64
		wantAddress  string
		wantDreamBal string
		wantErr      error
	}{
		{
			name: "ReturnsFirstMemberForTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "member1", types.TrustLevel_TRUST_LEVEL_NEW)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member2", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member3", types.TrustLevel_TRUST_LEVEL_NEW)
			},
			trustLevel:   uint64(types.TrustLevel_TRUST_LEVEL_NEW),
			wantAddress:  "member1",
			wantDreamBal: "1000",
		},
		{
			name: "EmptyResponseWhenNoMembersForTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "member1", types.TrustLevel_TRUST_LEVEL_NEW)
				createMemberWithTrustLevel(f.keeper, f.ctx, "member2", types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
			},
			trustLevel: uint64(types.TrustLevel_TRUST_LEVEL_CORE),
			wantErr:    nil,
		},
		{
			name:       "EmptyResponseWhenNoMembersExist",
			setup:      func(f *fixture) {},
			trustLevel: uint64(types.TrustLevel_TRUST_LEVEL_NEW),
			wantErr:    nil,
		},
		{
			name: "ReturnsMemberWithAdminTrustLevel",
			setup: func(f *fixture) {
				createMemberWithTrustLevel(f.keeper, f.ctx, "admin1", types.TrustLevel_TRUST_LEVEL_CORE)
				createMemberWithTrustLevel(f.keeper, f.ctx, "admin2", types.TrustLevel_TRUST_LEVEL_CORE)
			},
			trustLevel:   uint64(types.TrustLevel_TRUST_LEVEL_CORE),
			wantAddress:  "admin1",
			wantDreamBal: "1000",
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
			} else if tc.wantAddress != "" {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantAddress, response.Address)
				require.Equal(t, tc.wantDreamBal, response.DreamBalance.String())
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, "", response.Address)
				if response.DreamBalance != nil {
					require.Equal(t, "0", response.DreamBalance.String())
				}
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

	// Query should return first member (contributor1)
	response, err := qs.MembersByTrustLevel(f.ctx, &types.QueryMembersByTrustLevelRequest{
		TrustLevel: uint64(types.TrustLevel_TRUST_LEVEL_ESTABLISHED),
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, "contributor1", response.Address)
	require.Equal(t, "1000", response.DreamBalance.String())
}

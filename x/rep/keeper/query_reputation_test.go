package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createMemberWithReputation(k keeper.Keeper, ctx context.Context, address string, score string, lifetime string) {
	member := types.Member{
		Address:        address,
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(500)),
		LifetimeEarned: PtrInt(math.NewInt(10000)),
		LifetimeBurned: PtrInt(math.NewInt(100)),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{
			"development": score,
			"design":      "75.5",
		},
		LifetimeReputation: map[string]string{
			"development": lifetime,
			"design":      "150.0",
		},
	}
	_ = k.Member.Set(ctx, address, member)
}

func TestReputation(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*fixture)
		address      string
		tag          string
		wantScore    string
		wantLifetime string
		wantErr      error
	}{
		{
			name: "ReturnsReputationForExistingTag",
			setup: func(f *fixture) {
				createMemberWithReputation(f.keeper, f.ctx, "member1", "85.5", "125.0")
			},
			address:      "member1",
			tag:          "development",
			wantScore:    "85.500000000000000000",
			wantLifetime: "125.000000000000000000",
		},
		{
			name: "ReturnsZeroForNonExistentTag",
			setup: func(f *fixture) {
				createMemberWithReputation(f.keeper, f.ctx, "member1", "85.5", "125.0")
			},
			address:      "member1",
			tag:          "marketing",
			wantScore:    "0.000000000000000000",
			wantLifetime: "0.000000000000000000",
		},
		{
			name:    "MemberNotFound",
			address: "generate_valid",
			setup:   func(f *fixture) {},
			tag:     "development",
			wantErr: status.Error(codes.NotFound, "member not found"),
		},
		{
			name: "ReturnsReputationForDesignTag",
			setup: func(f *fixture) {
				createMemberWithReputation(f.keeper, f.ctx, "designer1", "90.0", "200.0")
			},
			address:      "designer1",
			tag:          "design",
			wantScore:    "75.500000000000000000",
			wantLifetime: "150.000000000000000000",
		},
		{
			name:    "InvalidRequestNil",
			setup:   func(f *fixture) {},
			address: "",
			tag:     "",
			wantErr: status.Error(codes.InvalidArgument, "invalid request"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			if tc.setup != nil {
				tc.setup(f)
			}

			var req *types.QueryReputationRequest
			if tc.wantErr == nil || tc.address == "generate_valid" {
				addr := tc.address
				if addr == "generate_valid" {
					addr, _ = f.addressCodec.BytesToString(sdk.AccAddress([]byte("nonexistent")))
				}
				req = &types.QueryReputationRequest{
					Address: addr,
					Tag:     tc.tag,
				}
			}

			response, err := qs.Reputation(f.ctx, req)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.Equal(t, tc.wantScore, response.Score.String())
				require.Equal(t, tc.wantLifetime, response.Lifetime.String())
			}
		})
	}
}

func TestReputation_MultipleTags(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create member with multiple reputation tags
	member := types.Member{
		Address:        "multi-skilled",
		DreamBalance:   PtrInt(math.NewInt(5000)),
		StakedDream:    PtrInt(math.NewInt(1000)),
		LifetimeEarned: PtrInt(math.NewInt(50000)),
		LifetimeBurned: PtrInt(math.NewInt(200)),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_TRUSTED,
		ReputationScores: map[string]string{
			"development": "95.0",
			"design":      "88.5",
			"research":    "92.0",
		},
		LifetimeReputation: map[string]string{
			"development": "350.0",
			"design":      "220.0",
			"research":    "180.0",
		},
	}
	_ = f.keeper.Member.Set(f.ctx, "multi-skilled", member)

	// Query each tag individually
	devResponse, err := qs.Reputation(f.ctx, &types.QueryReputationRequest{
		Address: "multi-skilled",
		Tag:     "development",
	})
	require.NoError(t, err)
	require.Equal(t, "95.000000000000000000", devResponse.Score.String())
	require.Equal(t, "350.000000000000000000", devResponse.Lifetime.String())

	designResponse, err := qs.Reputation(f.ctx, &types.QueryReputationRequest{
		Address: "multi-skilled",
		Tag:     "design",
	})
	require.NoError(t, err)
	require.Equal(t, "88.500000000000000000", designResponse.Score.String())
	require.Equal(t, "220.000000000000000000", designResponse.Lifetime.String())
}

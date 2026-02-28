package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestQueryIsNullifierUsed(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	queryServer := keeper.NewQueryServerImpl(k)

	tests := []struct {
		name        string
		setup       func()
		req         *types.QueryIsNullifierUsedRequest
		expectError bool
		expectUsed  bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "unused nullifier",
			req: &types.QueryIsNullifierUsedRequest{
				Domain:       1,
				Scope:        1,
				NullifierHex: "abcdef1234567890",
			},
			expectUsed: false,
		},
		{
			name: "used nullifier",
			setup: func() {
				entry := types.AnonNullifierEntry{
					UsedAt: 1000,
					Domain: 1,
					Scope:  2,
				}
				k.SetNullifierUsed(ctx, 1, 2, "deadbeef", entry)
			},
			req: &types.QueryIsNullifierUsedRequest{
				Domain:       1,
				Scope:        2,
				NullifierHex: "deadbeef",
			},
			expectUsed: true,
		},
		{
			name: "different domain not used",
			setup: func() {
				entry := types.AnonNullifierEntry{
					UsedAt: 2000,
					Domain: 5,
					Scope:  10,
				}
				k.SetNullifierUsed(ctx, 5, 10, "aabbccdd", entry)
			},
			req: &types.QueryIsNullifierUsedRequest{
				Domain:       99,
				Scope:        10,
				NullifierHex: "aabbccdd",
			},
			expectUsed: false,
		},
		{
			name: "different scope not used",
			setup: func() {
				entry := types.AnonNullifierEntry{
					UsedAt: 3000,
					Domain: 7,
					Scope:  20,
				}
				k.SetNullifierUsed(ctx, 7, 20, "11223344", entry)
			},
			req: &types.QueryIsNullifierUsedRequest{
				Domain:       7,
				Scope:        99,
				NullifierHex: "11223344",
			},
			expectUsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			resp, err := queryServer.IsNullifierUsed(ctx, tt.req)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.expectUsed, resp.Used)
		})
	}
}

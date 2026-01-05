package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgCreateMarket(t *testing.T) {
	// Create test addresses
	alice := sdk.AccAddress([]byte("alice"))

	tests := []struct {
		name       string
		msg        types.MsgCreateMarket
		expectErr  bool
		checkState func(t *testing.T, k keeper.Keeper, m *MockBankKeeper, ctx context.Context, res *types.MsgCreateMarketResponse)
	}{
		{
			name: "Success - Governance proposal outcome market",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "PROP-42",
				Question:         "Will governance proposal #42 pass?",
				InitialLiquidity: "100000uspark",
				EndBlock:         110, // Current height assumed 10 (initFixture default? We will check ctx)
			},
			expectErr: false,
		},
		{
			name: "Error - Insufficient Funds",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "BROKE",
				Question:         "Am I broke?",
				InitialLiquidity: "999999uspark", // More than alice has
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Duration (EndBlock <= Current)",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "INVALID-TIME",
				Question:         "Time travel?",
				InitialLiquidity: "1000uspark",
				// In test loop, we set context height. We'll set it to 10.
				// So 10 is invalid.
				EndBlock: 10,
			},
			expectErr: true,
		},
		{
			name: "Error - Past Duration",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "INVALID-PAST",
				Question:         "Past?",
				InitialLiquidity: "1000uspark",
				EndBlock:         5, // 5 < 10
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Liquidity Coin",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "BAD-COIN",
				Question:         "Bad money?",
				InitialLiquidity: "invalid-coin-format",
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Creator Address",
			msg: types.MsgCreateMarket{
				Creator:          "invalid-address",
				Symbol:           "BAD-ADDR",
				Question:         "Who am I?",
				InitialLiquidity: "1000uspark",
				EndBlock:         1000,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Init fresh fixture
			f := initFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			ctx := sdk.UnwrapSDKContext(f.ctx)

			// Set a fixed block height for consistent testing
			ctx = ctx.WithBlockHeight(10)

			// Fund Alice
			f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(100000)))

			res, err := ms.CreateMarket(ctx, &tc.msg)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)

				if tc.checkState != nil {
					tc.checkState(t, f.keeper, f.bankKeeper, ctx, res)
				}
			}
		})
	}
}

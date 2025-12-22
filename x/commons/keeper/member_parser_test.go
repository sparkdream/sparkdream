package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

func TestParseMembers(t *testing.T) {
	// 1. Setup minimal dependencies
	// parseMembers is a pure function attached to Keeper, so we don't need a full store/app setup.
	k := Keeper{}

	// Helper to generate valid addresses
	createAddr := func(val string) string {
		addr, err := bech32.ConvertAndEncode("cosmos", []byte(val))
		require.NoError(t, err)
		return addr
	}

	addr1 := createAddr("address1________________") // Must be 20 or 32 bytes usually, but bech32 logic just checks format
	addr2 := createAddr("address2________________")

	tests := []struct {
		name          string
		members       []string
		weights       []string
		expectedLen   int
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid input single",
			members:     []string{addr1},
			weights:     []string{"1"},
			expectedLen: 1,
			expectError: false,
		},
		{
			name:        "valid input multiple",
			members:     []string{addr1, addr2},
			weights:     []string{"1", "10"},
			expectedLen: 2,
			expectError: false,
		},
		{
			name:          "mismatch length",
			members:       []string{addr1},
			weights:       []string{"1", "2"},
			expectError:   true,
			errorContains: "members count (1) does not match weights count (2)",
		},
		{
			name:          "invalid address",
			members:       []string{"invalid_bech32_address"},
			weights:       []string{"1"},
			expectError:   true,
			errorContains: "invalid member address",
		},
		{
			name:        "empty input",
			members:     []string{},
			weights:     []string{},
			expectedLen: 0,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := k.parseMembers(tc.members, tc.weights)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorContains)
				require.Nil(t, res)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedLen, len(res))

				// Verify content mapping
				if tc.expectedLen > 0 {
					require.Equal(t, tc.members[0], res[0].Address)
					require.Equal(t, tc.weights[0], res[0].Weight)
					require.Equal(t, "Added via x/commons", res[0].Metadata)
				}
			}
		})
	}
}

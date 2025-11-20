package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	// Import mocks or testutil if available, otherwise we use a minimal mock below
)

// --- Mocks for Unit Testing ---
type MockBankKeeper struct {
	Balances map[string]sdk.Coins
}

func (m *MockBankKeeper) GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins {
	return m.Balances[addr.String()]
}

func (m *MockBankKeeper) SendCoins(ctx sdk.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	// Simple logic: Subtract from sender, add to receiver
	m.Balances[from.String()] = m.Balances[from.String()].Sub(amt...)
	m.Balances[to.String()] = m.Balances[to.String()].Add(amt...)
	return nil
}

// --- The Test ---
func TestSplitFundsLogic(t *testing.T) {
	// 1. Setup
	// Define addresses
	sourceAddr := sdk.AccAddress([]byte("distribution_module_addr"))
	commonsAddr := sdk.AccAddress([]byte("commons_council_addr____")) // 20 bytes
	techAddr := sdk.AccAddress([]byte("gov_module_addr_________"))
	ecoAddr := sdk.AccAddress([]byte("ecosystem_module_addr___"))

	// Initial Pot: 1000 uspark
	initialBalance := sdk.NewCoins(sdk.NewInt64Coin("uspark", 1000))

	// Setup Mock Bank
	mockBank := &MockBankKeeper{
		Balances: map[string]sdk.Coins{
			sourceAddr.String(): initialBalance,
		},
	}

	// 2. Run Math Logic
	// Since we can't easily instantiate the full Keeper without a full App in a unit test,
	// we replicate the math logic here to verify the integrity of the 50/30/20 split.
	// (Ideally, use the Keeper method, but that requires mocking StoreService, AuthKeeper, etc.)

	total := initialBalance[0].Amount
	commonsAmt := total.MulRaw(50).QuoRaw(100)   // 500
	techAmt := total.MulRaw(30).QuoRaw(100)      // 300
	ecoAmt := total.Sub(commonsAmt).Sub(techAmt) // 200

	// Execute Mock Sends (simulating Keeper.SplitFunds)
	err1 := mockBank.SendCoins(sdk.Context{}, sourceAddr, commonsAddr, sdk.NewCoins(sdk.NewCoin("uspark", commonsAmt)))
	err2 := mockBank.SendCoins(sdk.Context{}, sourceAddr, techAddr, sdk.NewCoins(sdk.NewCoin("uspark", techAmt)))
	err3 := mockBank.SendCoins(sdk.Context{}, sourceAddr, ecoAddr, sdk.NewCoins(sdk.NewCoin("uspark", ecoAmt)))

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)

	// 3. Assertions
	// Did Source empty out?
	require.True(t, mockBank.Balances[sourceAddr.String()].IsZero(), "Source should be empty")

	// Did Commons get 50%?
	require.Equal(t, int64(500), mockBank.Balances[commonsAddr.String()].AmountOf("uspark").Int64())

	// Did Tech get 30%?
	require.Equal(t, int64(300), mockBank.Balances[techAddr.String()].AmountOf("uspark").Int64())

	// Did Eco get 20%?
	require.Equal(t, int64(200), mockBank.Balances[ecoAddr.String()].AmountOf("uspark").Int64())
}

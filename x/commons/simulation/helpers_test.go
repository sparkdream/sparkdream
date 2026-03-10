package simulation

import (
	"math/rand"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"
)

// --- randomFees ---

func TestRandomFees_EmptyCoins(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ctx := sdk.Context{}

	fees, err := randomFees(r, ctx, sdk.Coins{})
	require.NoError(t, err)
	require.Nil(t, fees)
}

func TestRandomFees_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	ctx := sdk.Context{}

	spendable := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000)))
	fees, err := randomFees(r, ctx, spendable)
	require.NoError(t, err)
	require.True(t, fees.AmountOf("uspark").IsPositive())
	require.True(t, fees.AmountOf("uspark").LTE(math.NewInt(1000000)))
}

func TestRandomFees_MultiDenom(t *testing.T) {
	r := rand.New(rand.NewSource(99))
	ctx := sdk.Context{}

	spendable := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(500)),
		sdk.NewCoin("udream", math.NewInt(300)),
	)
	fees, err := randomFees(r, ctx, spendable)
	require.NoError(t, err)
	require.True(t, len(fees) > 0)
}

func TestRandomFees_IsDeterministic(t *testing.T) {
	ctx := sdk.Context{}
	spendable := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000)))

	r1 := rand.New(rand.NewSource(77))
	r2 := rand.New(rand.NewSource(77))

	fees1, err1 := randomFees(r1, ctx, spendable)
	fees2, err2 := randomFees(r2, ctx, spendable)
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.True(t, fees1.Equal(fees2))
}

func TestRandomFees_ZeroAmountCoin(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	ctx := sdk.Context{}

	spendable := sdk.Coins{sdk.NewCoin("uspark", math.NewInt(0))}
	fees, err := randomFees(r, ctx, spendable)
	require.NoError(t, err)
	// Zero-amount coin is skipped
	require.True(t, fees.IsZero() || fees == nil)
}

// --- getGroupByPolicy ---
// getGroupByPolicy requires a real keeper with store, so it's tested
// indirectly via the simulation operations (integration tests).
// Unit testing it would require the full keeper test fixture.

// --- Simulation Account Helpers ---

func TestRandomAcc_Deterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(10))
	r2 := rand.New(rand.NewSource(10))
	accs := simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 5)

	acc1, _ := simtypes.RandomAcc(r1, accs)
	acc2, _ := simtypes.RandomAcc(r2, accs)
	require.Equal(t, acc1.Address, acc2.Address)
}

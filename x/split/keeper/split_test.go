package keeper_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/split/types"
)

// mockDistr is a community pool that pays out until it runs dry.
type mockDistr struct {
	pool     sdk.Coins
	received map[string]sdk.Coins
}

func (m *mockDistr) GetCommunityPool(context.Context) (sdk.DecCoins, error) {
	return sdk.NewDecCoinsFromCoins(m.pool...), nil
}

func (m *mockDistr) DistributeFromFeePool(_ context.Context, amount sdk.Coins, to sdk.AccAddress) error {
	if !m.pool.IsAllGTE(amount) {
		return errors.New("insufficient community pool")
	}
	m.pool = m.pool.Sub(amount...)
	m.received[to.String()] = m.received[to.String()].Add(amount...)
	return nil
}

func splitAddr(s string) string {
	b := make([]byte, 20)
	copy(b, s)
	return sdk.AccAddress(b).String()
}

// SplitFunds itself (not a re-implementation) divides the pool by weight.
func TestSplitFunds_DistributesByWeight(t *testing.T) {
	f := initFixture(t)
	distr := &mockDistr{pool: sdk.NewCoins(sdk.NewInt64Coin("uspark", 10_000_000)), received: map[string]sdk.Coins{}}
	f.keeper.SetDistrKeeper(distr)

	commons, tech, eco := splitAddr("commons"), splitAddr("tech"), splitAddr("eco")
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, commons, 50))
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, tech, 30))
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, eco, 20))

	require.NoError(t, f.keeper.SplitFunds(f.ctx))
	require.Equal(t, int64(5_000_000), distr.received[commons].AmountOf("uspark").Int64())
	require.Equal(t, int64(3_000_000), distr.received[tech].AmountOf("uspark").Int64())
	require.Equal(t, int64(2_000_000), distr.received[eco].AmountOf("uspark").Int64())
}

// Below the per-denom threshold nothing moves.
func TestSplitFunds_Threshold(t *testing.T) {
	f := initFixture(t)
	distr := &mockDistr{pool: sdk.NewCoins(sdk.NewInt64Coin("uspark", 999_999)), received: map[string]sdk.Coins{}}
	f.keeper.SetDistrKeeper(distr)
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("commons"), 10))
	require.NoError(t, f.keeper.SplitFunds(f.ctx))
	require.Empty(t, distr.received)
}

// SPL-A01: only the bond denom is distributed, so donated foreign denoms
// (above or below the threshold) cause no transfers and stay in the pool.
func TestSplitFunds_BondDenomOnly(t *testing.T) {
	f := initFixture(t)
	pool := sdk.NewCoins(sdk.NewInt64Coin("uspark", 4_000_000))
	for i := 0; i < 500; i++ {
		pool = pool.Add(sdk.NewInt64Coin(fmt.Sprintf("ibc/%064d", i), 5_000_000))
	}
	distr := &mockDistr{pool: pool, received: map[string]sdk.Coins{}}
	counting := &countingDistr{mockDistr: distr}
	f.keeper.SetDistrKeeper(counting)

	a, b := splitAddr("commons"), splitAddr("tech")
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, a, 1))
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, b, 1))
	require.NoError(t, f.keeper.SplitFunds(f.ctx))

	require.Equal(t, 2, counting.calls, "one transfer per recipient, none per foreign denom")
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uspark", 2_000_000)), distr.received[a])
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uspark", 2_000_000)), distr.received[b])
	require.True(t, distr.pool.AmountOf("uspark").IsZero())
	require.Len(t, distr.pool, 500, "foreign denoms stay in the pool")
}

// No identity keeper wired: SplitFunds logs and skips instead of panicking.
func TestSplitFunds_IdentityNotWired(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetIdentityKeeper(nil)
	distr := &mockDistr{pool: sdk.NewCoins(sdk.NewInt64Coin("uspark", 10_000_000)), received: map[string]sdk.Coins{}}
	f.keeper.SetDistrKeeper(distr)
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("commons"), 10))
	require.NoError(t, f.keeper.SplitFunds(f.ctx))
	require.Empty(t, distr.received)
}

type countingDistr struct {
	*mockDistr
	calls int
}

func (c *countingDistr) DistributeFromFeePool(ctx context.Context, amount sdk.Coins, to sdk.AccAddress) error {
	c.calls++
	return c.mockDistr.DistributeFromFeePool(ctx, amount, to)
}

// SPL-01: weights are capped per share and in total, and errors surface.
func TestSetShareByAddress_Caps(t *testing.T) {
	f := initFixture(t)
	require.ErrorIs(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("a"), 0), types.ErrInvalidShare)
	require.ErrorIs(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("a"), types.MaxShareWeight+1), types.ErrInvalidShare)
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("a"), 9_000))
	require.ErrorIs(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("b"), 1_001), types.ErrInvalidShare, "total above MaxTotalShareWeight")
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("a"), 10_000), "replacing a share counts it once")
}

// SPL-02: a short pool skips the recipient it cannot pay and keeps going.
func TestSplitFunds_ShortPoolContinues(t *testing.T) {
	f := initFixture(t)
	distr := &mockDistr{pool: sdk.NewCoins(sdk.NewInt64Coin("uspark", 2_000_000)), received: map[string]sdk.Coins{}}
	f.keeper.SetDistrKeeper(distr)
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("one"), 1))
	require.NoError(t, f.keeper.SetShareByAddress(f.ctx, splitAddr("two"), 1))
	// The first payout fails; the loop must not stop there.
	inner := &shrinkingDistr{mockDistr: distr}
	f.keeper.SetDistrKeeper(inner)
	require.NoError(t, f.keeper.SplitFunds(f.ctx))
	require.Equal(t, 2, inner.calls, "both recipients were attempted")
}

type shrinkingDistr struct {
	*mockDistr
	calls int
}

func (s *shrinkingDistr) DistributeFromFeePool(ctx context.Context, amount sdk.Coins, to sdk.AccAddress) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("community pool short")
	}
	return s.mockDistr.DistributeFromFeePool(ctx, amount, to)
}

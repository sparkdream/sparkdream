package dreamutil

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// mockDREAMKeeper records calls for verification.
type mockDREAMKeeper struct {
	locked   map[string]math.Int
	unlocked map[string]math.Int
	burned   map[string]math.Int
	failOn   string // raw string key to simulate failure on
}

func newMockDREAMKeeper() *mockDREAMKeeper {
	return &mockDREAMKeeper{
		locked:   make(map[string]math.Int),
		unlocked: make(map[string]math.Int),
		burned:   make(map[string]math.Int),
	}
}

func addrKey(addr sdk.AccAddress) string {
	return string(addr)
}

func (m *mockDREAMKeeper) LockDREAM(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
	key := addrKey(addr)
	if key == m.failOn {
		return fmt.Errorf("mock lock failure")
	}
	m.locked[key] = amount
	return nil
}

func (m *mockDREAMKeeper) UnlockDREAM(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
	key := addrKey(addr)
	if key == m.failOn {
		return fmt.Errorf("mock unlock failure")
	}
	m.unlocked[key] = amount
	return nil
}

func (m *mockDREAMKeeper) BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error {
	if err := m.UnlockDREAM(ctx, addr, amount); err != nil {
		return err
	}
	return m.BurnDREAM(ctx, addr, amount)
}

func (m *mockDREAMKeeper) BurnDREAM(_ context.Context, addr sdk.AccAddress, amount math.Int) error {
	key := addrKey(addr)
	if key == m.failOn {
		return fmt.Errorf("mock burn failure")
	}
	m.burned[key] = amount
	return nil
}

// mockAddressCodec converts between string and byte addresses using raw bytes.
type mockAddressCodec struct{}

func (mockAddressCodec) StringToBytes(text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("empty address")
	}
	return []byte(text), nil
}

func (mockAddressCodec) BytesToString(bz []byte) (string, error) {
	return string(bz), nil
}

func TestOps_Lock(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	err := ops.Lock(ctx, "alice", 100)
	require.NoError(t, err)
	require.Equal(t, math.NewIntFromUint64(100), mock.locked["alice"])
}

func TestOps_Unlock(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	err := ops.Unlock(ctx, "bob", 200)
	require.NoError(t, err)
	require.Equal(t, math.NewIntFromUint64(200), mock.unlocked["bob"])
}

func TestOps_Burn(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	err := ops.Burn(ctx, "carol", 50)
	require.NoError(t, err)
	require.Equal(t, math.NewIntFromUint64(50), mock.burned["carol"])
}

func TestOps_NilKeeper(t *testing.T) {
	// NAME-S2-8: dreamutil panics rather than silently no-opping when wired
	// without a rep keeper, so misconfigured deployments surface a fatal
	// error instead of pretending to escrow/burn.
	ops := NewOps(nil, mockAddressCodec{})
	ctx := context.Background()

	require.Panics(t, func() { _ = ops.Lock(ctx, "alice", 100) })
	require.Panics(t, func() { _ = ops.Unlock(ctx, "alice", 100) })
	require.Panics(t, func() { _ = ops.Burn(ctx, "alice", 100) })
	require.Panics(t, func() { _ = ops.SettleStakes(ctx, "alice", 100, "bob", 200) })
}

func TestOps_InvalidAddress(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	require.Error(t, ops.Lock(ctx, "", 100))
	require.Error(t, ops.Unlock(ctx, "", 100))
	require.Error(t, ops.Burn(ctx, "", 100))
}

func TestOps_SettleStakes(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	err := ops.SettleStakes(ctx, "winner", 50, "loser", 100)
	require.NoError(t, err)
	require.Equal(t, math.NewIntFromUint64(50), mock.unlocked["winner"])
	require.Equal(t, math.NewIntFromUint64(100), mock.burned["loser"])
}

func TestOps_SettleStakes_UnlockFailure(t *testing.T) {
	mock := newMockDREAMKeeper()
	mock.failOn = "winner"
	ops := NewOps(mock, mockAddressCodec{})
	ctx := context.Background()

	err := ops.SettleStakes(ctx, "winner", 50, "loser", 100)
	require.Error(t, err)
	// Loser should NOT be burned if unlock failed
	_, loserBurned := mock.burned["loser"]
	require.False(t, loserBurned)
}

// The loser's stake is locked: settling it releases the lock before burning,
// so the burn comes out of the stake and not the loser's free balance.
func TestOps_SettleStakesBurnsTheLoserLock(t *testing.T) {
	mock := newMockDREAMKeeper()
	ops := NewOps(mock, mockAddressCodec{})
	require.NoError(t, ops.SettleStakes(context.Background(), "phoenix", 10, "zenith", 25))
	require.Equal(t, math.NewInt(10), mock.unlocked["phoenix"])
	require.Equal(t, math.NewInt(25), mock.unlocked["zenith"], "loser lock released")
	require.Equal(t, math.NewInt(25), mock.burned["zenith"])
}

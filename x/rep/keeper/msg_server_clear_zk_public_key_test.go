package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func zkKey(last byte) []byte {
	k := make([]byte, 32)
	k[31] = last
	return k
}

// TestClearZkPublicKey_RequiresAKey: clearing an empty slot is a no-op error.
func TestClearZkPublicKey_RequiresAKey(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("clear-none_________"))
	seedMigrant(t, f, a, 1)
	srv := keeper.NewMsgServerImpl(f.keeper)
	_, err := srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: a.String()})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

// TestClearZkPublicKey_ClearThenRegisterIsNotAFreeSwap pins the rule from the
// multisig-accounts plan §2.1: after a clear the next registration is never
// immediate and the season cap still applies.
func TestClearZkPublicKey_ClearThenRegisterIsNotAFreeSwap(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("clear-swap_________"))
	seedMigrant(t, f, a, 1)
	srv := keeper.NewMsgServerImpl(f.keeper)
	p := mustParams(t, f)

	// First registration is immediate (unchanged behaviour).
	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(1)})
	require.NoError(t, err)
	require.Equal(t, zkKey(1), mustMember(t, f, a).ZkPublicKey)

	// Clear: leaf gone at once, flag set, tree dirty, season markers untouched.
	_, err = srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: a.String()})
	require.NoError(t, err)
	m := mustMember(t, f, a)
	require.Empty(t, m.ZkPublicKey)
	require.Empty(t, m.PendingZkPublicKey)
	require.True(t, m.ZkKeyCleared)
	require.False(t, m.ZkKeyChanged, "a clear is not a change")
	require.True(t, f.keeper.IsTrustTreeDirty(f.ctx))

	// Register after clear: NOT immediate, the key waits out the delay.
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(2)})
	require.NoError(t, err)
	m = mustMember(t, f, a)
	require.Empty(t, m.ZkPublicKey, "no instant swap after a clear")
	require.Equal(t, zkKey(2), m.PendingZkPublicKey)
	require.True(t, m.ZkKeyChanged, "this registration uses the season change")

	// Activation consumes the flag.
	advance(f, 0, p.ZkKeyActivationDelay*p.EpochBlocks)
	f.keeper.ProcessMigrations(f.ctx)
	m = mustMember(t, f, a)
	require.Equal(t, zkKey(2), m.ZkPublicKey)
	require.False(t, m.ZkKeyCleared, "flag consumed by activation")

	// Season change already used: a second clear is still allowed (no limit)...
	_, err = srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: a.String()})
	require.NoError(t, err)
	require.Empty(t, mustMember(t, f, a).ZkPublicKey)
	// ...but registering again this season is refused: safe failure, the
	// member stays without anonymity rather than gaining a second swap.
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(3)})
	require.ErrorIs(t, err, types.ErrZkKeyCooldown)
}

// TestClearZkPublicKey_DropsPendingActivation: clearing while a change is
// parked removes the queue entry so the parked key never activates.
func TestClearZkPublicKey_DropsPendingActivation(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("clear-pending______"))
	seedMigrant(t, f, a, 1)
	srv := keeper.NewMsgServerImpl(f.keeper)
	p := mustParams(t, f)

	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(1)})
	require.NoError(t, err)
	_, err = srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(2)})
	require.NoError(t, err)
	m := mustMember(t, f, a)
	require.Equal(t, zkKey(2), m.PendingZkPublicKey)
	has, err := f.keeper.ZkKeyActivationQueue.Has(f.ctx, collections.Join(m.ZkKeyActivatesAtEpoch, a.String()))
	require.NoError(t, err)
	require.True(t, has)
	activatesAt := m.ZkKeyActivatesAtEpoch

	_, err = srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: a.String()})
	require.NoError(t, err)
	has, err = f.keeper.ZkKeyActivationQueue.Has(f.ctx, collections.Join(activatesAt, a.String()))
	require.NoError(t, err)
	require.False(t, has, "queue entry removed")

	advance(f, 0, p.ZkKeyActivationDelay*p.EpochBlocks)
	f.keeper.ProcessMigrations(f.ctx)
	m = mustMember(t, f, a)
	require.Empty(t, m.ZkPublicKey, "the parked key never activates")
	require.True(t, m.ZkKeyCleared)
}

// TestClearZkPublicKey_RefusesTombstone: a migrated-away address cannot touch
// the key that now belongs to the new address.
func TestClearZkPublicKey_RefusesTombstone(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("clear-tomb-a_______"))
	b := sdk.AccAddress([]byte("clear-tomb-b_______"))
	seedMigrant(t, f, a, 1)
	srv := keeper.NewMsgServerImpl(f.keeper)
	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{Member: a.String(), ZkPublicKey: zkKey(1)})
	require.NoError(t, err)
	finalizeKeyMigration(t, f, a, b)
	_, err = srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: a.String()})
	require.Error(t, err)
	// The new address holds the identity and may clear.
	_, err = srv.ClearZkPublicKey(f.ctx, &types.MsgClearZkPublicKey{Member: b.String()})
	require.NoError(t, err)
}

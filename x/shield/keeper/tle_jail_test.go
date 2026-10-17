package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

type mockSlashingKeeper struct {
	jailed    []sdk.ConsAddress
	jailUntil map[string]time.Time
}

func (m *mockSlashingKeeper) Jail(_ context.Context, cons sdk.ConsAddress) error {
	m.jailed = append(m.jailed, cons)
	return nil
}

func (m *mockSlashingKeeper) JailUntil(_ context.Context, cons sdk.ConsAddress, t time.Time) error {
	if m.jailUntil == nil {
		m.jailUntil = map[string]time.Time{}
	}
	m.jailUntil[cons.String()] = t
	return nil
}

// TestTLEJailAppliesDuration: past tle_miss_tolerance the validator is
// jailed and JailUntil is set tle_jail_duration seconds past block time.
func TestTLEJailAppliesDuration(t *testing.T) {
	f := initFixture(t)
	pk := ed25519.GenPrivKey().PubKey()
	valAddr := sdk.ValAddress([]byte("zenith-validator----"))
	val, err := stakingtypes.NewValidator(valAddr.String(), pk, stakingtypes.Description{Moniker: "zenith"})
	require.NoError(t, err)
	val.Status = stakingtypes.Bonded // only bonded validators are judged
	f.keeper.SetStakingKeeper(mockStakingKeeper{validators: []stakingtypes.Validator{val}})
	slashing := &mockSlashingKeeper{}
	f.keeper.SetSlashingKeeper(slashing)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 1
	params.TleJailDuration = 900
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("mpk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{{ValidatorAddress: valAddr.String(), PublicShare: []byte("s"), ShareIndex: 1}},
	}))

	blockTime := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	ctx := f.ctx.WithBlockTime(blockTime)
	// Two demanded epochs, both unserved: the second judgement exceeds the
	// tolerance of one.
	for epoch := uint64(1); epoch <= 2; epoch++ {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(ctx, types.ShieldEpochState{CurrentEpoch: epoch, EpochStartHeight: int64(epoch * 10)}))
		require.NoError(t, f.keeper.SetDecryptionDemand(ctx, epoch, int64(epoch*10)))
		ctx = ctx.WithBlockHeight(int64((epoch + 1) * 10))
		require.NoError(t, f.keeper.EndBlocker(ctx))
	}

	cons, err := val.GetConsAddr()
	require.NoError(t, err)
	require.Len(t, slashing.jailed, 1)
	require.Equal(t, sdk.ConsAddress(cons), slashing.jailed[0])
	require.Equal(t, blockTime.Add(900*time.Second), slashing.jailUntil[sdk.ConsAddress(cons).String()])

	var jailEvent bool
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == types.EventTypeTLEJail {
			jailEvent = true
		}
	}
	require.True(t, jailEvent)
}

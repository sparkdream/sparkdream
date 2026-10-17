package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestTLELivenessSkipsUnbondedAndUnknownValidators: a key-set validator that
// is no longer bonded, or that the staking keeper no longer knows, is not
// judged (no miss, no ring entry, no miss event); a bonded one in the same
// key set is.
func TestTLELivenessSkipsUnbondedAndUnknownValidators(t *testing.T) {
	f := initFixture(t)

	newVal := func(seed string, status stakingtypes.BondStatus) (stakingtypes.Validator, string) {
		addr := sdk.ValAddress([]byte(seed))
		v, err := stakingtypes.NewValidator(addr.String(), ed25519.GenPrivKey().PubKey(), stakingtypes.Description{Moniker: seed})
		require.NoError(t, err)
		v.Status = status
		return v, addr.String()
	}
	bonded, bondedAddr := newVal("phoenix-validator---", stakingtypes.Bonded)
	unbonded, unbondedAddr := newVal("aurora-validator----", stakingtypes.Unbonded)
	// Valid bech32 operator the staking keeper does not know (GetValidator errors).
	unknownAddr := sdk.ValAddress([]byte("zenith-validator----")).String()

	f.keeper.SetStakingKeeper(mockStakingKeeper{validators: []stakingtypes.Validator{bonded, unbonded}})

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("mpk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: bondedAddr, PublicShare: []byte("s1"), ShareIndex: 1},
			{ValidatorAddress: unbondedAddr, PublicShare: []byte("s2"), ShareIndex: 2},
			{ValidatorAddress: unknownAddr, PublicShare: []byte("s3"), ShareIndex: 3},
		},
	}))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1, EpochStartHeight: 0}))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 1, 0))

	ctx := f.ctx.WithBlockHeight(10).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.EndBlocker(ctx))
	require.True(t, f.keeper.IsDecryptionDemandJudged(ctx, 1))

	require.Equal(t, uint64(1), f.keeper.GetTLEMissCount(ctx, bondedAddr), "bonded validator is judged")
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, unbondedAddr), "unbonded validator is not judged")
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(ctx, unknownAddr), "unknown validator is not judged")

	var missed []string
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != types.EventTypeTLEMiss {
			continue
		}
		var epoch string
		for _, a := range ev.Attributes {
			switch a.Key {
			case types.AttributeKeyValidator:
				missed = append(missed, a.Value)
			case types.AttributeKeyEpoch:
				epoch = a.Value
			}
		}
		require.Equal(t, "1", epoch)
	}
	require.Equal(t, []string{bondedAddr}, missed, "only the bonded validator's miss is announced")
}

// TestTLELivenessMissEvent: an unserved demanded epoch emits a miss event
// naming the validator and the epoch; a served one emits none.
func TestTLELivenessMissEvent(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.ShieldEpochInterval = 10
	params.TleMissWindow = 10
	params.TleMissTolerance = 5
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
		MasterPublicKey: []byte("mpk"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "phoenix", PublicShare: []byte("s1"), ShareIndex: 1},
			{ValidatorAddress: "aurora", PublicShare: []byte("s2"), ShareIndex: 2},
		},
	}))
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 3, EpochStartHeight: 20}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{Epoch: 3, Validator: "phoenix", Share: []byte("s")}))
	require.NoError(t, f.keeper.SetDecryptionDemand(f.ctx, 3, 20))

	ctx := f.ctx.WithBlockHeight(30).WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.EndBlocker(ctx))

	var events []map[string]string
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == types.EventTypeTLEMiss {
			attrs := map[string]string{}
			for _, a := range ev.Attributes {
				attrs[a.Key] = a.Value
			}
			events = append(events, attrs)
		}
	}
	require.Len(t, events, 1)
	require.Equal(t, "aurora", events[0][types.AttributeKeyValidator])
	require.Equal(t, "3", events[0][types.AttributeKeyEpoch])
}

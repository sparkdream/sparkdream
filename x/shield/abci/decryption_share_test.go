package abci

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	abcitypes "github.com/cometbft/cometbft/abci/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/keeper"
	module "sparkdream/x/shield/module"
	"sparkdream/x/shield/types"
)

// --- minimal keeper fixture for the ABCI handlers ---

type accountKeeperStub struct{}

func (accountKeeperStub) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}
func (accountKeeperStub) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}
func (accountKeeperStub) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI { return nil }
func (accountKeeperStub) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}
func (accountKeeperStub) SetAccount(context.Context, sdk.AccountI) {}

type bankKeeperStub struct{}

func (bankKeeperStub) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}
func (bankKeeperStub) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (bankKeeperStub) SendCoinsFromModuleToModule(context.Context, string, string, sdk.Coins) error {
	return nil
}

type identityStub struct{}

func (identityStub) IsIdentityKeeper()                 {}
func (identityStub) BondDenom(context.Context) string  { return "uspark" }
func (identityStub) DreamDenom(context.Context) string { return "udream" }

// stakingStub maps consensus addresses to operator addresses and consensus
// public keys.
type stakingStub struct {
	byCons map[string]string
	pubs   map[string]cryptotypes.PubKey
}

func (s stakingStub) GetValidator(context.Context, sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, fmt.Errorf("unused")
}

func (s stakingStub) GetValidatorByConsAddr(_ context.Context, cons sdk.ConsAddress) (stakingtypes.Validator, error) {
	op, ok := s.byCons[string(cons)]
	if !ok {
		return stakingtypes.Validator{}, fmt.Errorf("unknown validator")
	}
	val := stakingtypes.Validator{OperatorAddress: op}
	if pk, ok := s.pubs[string(cons)]; ok {
		pkAny, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			return stakingtypes.Validator{}, err
		}
		val.ConsensusPubkey = pkAny
	}
	return val, nil
}

func (s stakingStub) GetBondedValidatorsByPower(context.Context) ([]stakingtypes.Validator, error) {
	return nil, nil
}

type shareFixture struct {
	ctx     sdk.Context
	k       keeper.Keeper
	master  kyber.Scalar
	secrets map[string]kyber.Scalar
	cons    map[string][]byte              // operator → consensus address
	privs   map[string]cryptotypes.PrivKey // operator → consensus key
	pubs    map[string]cryptotypes.PubKey  // consensus address → consensus key
	ks      types.TLEKeySet
}

// The ABCI requests in these tests carry no height and round 0, so the
// handlers check signatures made at height -1, round 0.
const testSignedHeight, testSignedRound = int64(-1), int32(0)

// sign signs ext's bytes as validator v's vote extension.
func (f *shareFixture) sign(v string, extBz []byte) []byte {
	priv, ok := f.privs[v]
	if !ok {
		return nil
	}
	signBytes, err := extensionSignBytes(extBz, testSignedHeight, testSignedRound, f.ctx.ChainID())
	if err != nil {
		panic(err)
	}
	sig, err := priv.Sign(signBytes)
	if err != nil {
		panic(err)
	}
	return sig
}

// vote is v's committed vote carrying ext, signed by v.
func (f *shareFixture) vote(v string, ext []byte) abcitypes.ExtendedVoteInfo {
	vi := commitVote(f.cons[v], ext)
	vi.ExtensionSignature = f.sign(v, ext)
	return vi
}

// signedExt is ext attributed to v, with v's signature.
func (f *shareFixture) signedExt(v string, ext *types.DKGVoteExtension) *types.ValidatorDKGExtension {
	bz, err := ext.Marshal()
	if err != nil {
		panic(err)
	}
	return &types.ValidatorDKGExtension{ValidatorAddress: f.cons[v], Extension: *ext, ExtensionSignature: f.sign(v, bz)}
}

// validateInjected runs validateInjectedDKGData at the test signing point.
func validateInjected(ctx sdk.Context, k keeper.Keeper, data *types.InjectedDKGData) error {
	return validateInjectedDKGData(ctx, k, data, testSignedHeight, testSignedRound)
}

func newShareFixture(t *testing.T, vals []string, threshold int) *shareFixture {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	k := keeper.NewKeeper(runtime.NewKVStoreService(key), encCfg.Codec,
		addresscodec.NewBech32Codec("sprkdrm"), authtypes.NewModuleAddress("gov"),
		accountKeeperStub{}, bankKeeperStub{})
	k.SetIdentityKeeper(identityStub{})
	require.NoError(t, k.InitGenesis(testCtx.Ctx, *types.DefaultGenesis()))

	f := &shareFixture{ctx: testCtx.Ctx, k: k, secrets: map[string]kyber.Scalar{}, cons: map[string][]byte{},
		privs: map[string]cryptotypes.PrivKey{}, pubs: map[string]cryptotypes.PubKey{}}
	byCons := map[string]string{}
	f.master = tle.Suite.G1().Scalar().Pick(random.New())
	poly := share.NewPriPoly(tle.Suite.G1(), threshold, f.master, random.New())
	pri := poly.Shares(len(vals))
	f.ks = types.TLEKeySet{
		MasterPublicKey:      tle.MasterPublicKeyG1(f.master),
		MasterPublicKeyG2:    tle.MasterPublicKeyG2(f.master),
		ThresholdNumerator:   uint64(threshold),
		ThresholdDenominator: uint64(len(vals)),
	}
	for i, v := range vals {
		f.secrets[v] = pri[i].V
		sum := sha256.Sum256([]byte(fmt.Sprintf("cons-%02d-%s", i, v)))
		priv := ed25519.GenPrivKeyFromSecret(sum[:])
		cons := []byte(priv.PubKey().Address())
		f.cons[v] = cons
		f.privs[v] = priv
		f.pubs[string(cons)] = priv.PubKey()
		byCons[string(cons)] = v
		g1, _ := tle.Suite.G1().Point().Mul(pri[i].V, nil).MarshalBinary()
		g2, _ := tle.Suite.G2().Point().Mul(pri[i].V, nil).MarshalBinary()
		f.ks.ValidatorShares = append(f.ks.ValidatorShares, &types.TLEValidatorPublicShare{
			ValidatorAddress: v, PublicShare: g1, PublicShareG2: g2, ShareIndex: uint32(i + 1),
		})
	}
	k.SetStakingKeeper(stakingStub{byCons: byCons, pubs: f.pubs})
	require.NoError(t, k.SetTLEKeySetVal(f.ctx, f.ks))
	require.NoError(t, k.SetDKGStateVal(f.ctx, types.DKGState{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE, ExpectedValidators: vals}))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, k.Params.Set(f.ctx, params))
	return f
}

func (f *shareFixture) injected(epoch uint64, shares map[string][]byte) *types.InjectedDKGData {
	data := &types.InjectedDKGData{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE}
	for v, sh := range shares {
		data.DecryptionShares = append(data.DecryptionShares, &types.InjectedDecryptionShare{
			ValidatorAddress: f.cons[v], Epoch: epoch, Share: sh,
		})
	}
	return data
}

// TestDecryptionSharePipeline drives shares through the three ABCI checks:
// a forged share is rejected by VerifyVoteExtension and ProcessProposal and
// dropped by the PreBlocker (with the invalid-share event), honest shares
// reconstruct a key that opens a client's ciphertext, and shares for an
// undemanded or open epoch are refused everywhere.
func TestDecryptionSharePipeline(t *testing.T) {
	vals := []string{"val1", "val2", "val3", "val4"}
	f := newShareFixture(t, vals, 3)
	require.NoError(t, f.k.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 6, EpochStartHeight: 60}))
	const epoch = uint64(5)
	require.NoError(t, f.k.SetDecryptionDemand(f.ctx, epoch, 60))

	good := func(v string) []byte { return tle.DecryptionShare(f.secrets[v], epoch) }
	forged, _ := tle.Suite.G1().Point().Pick(random.New()).MarshalBinary()

	h := &DKGVoteExtensionHandler{keeper: f.k}
	voteExt := func(v string, ep uint64, sh []byte) abcitypes.ResponseVerifyVoteExtension_VerifyStatus {
		ext := types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE, DecryptionEpoch: ep, DecryptionShare: sh}
		bz, err := ext.Marshal()
		require.NoError(t, err)
		resp, err := h.VerifyVoteExtensionHandler()(f.ctx, &abcitypes.RequestVerifyVoteExtension{VoteExtension: bz, ValidatorAddress: f.cons[v]})
		require.NoError(t, err)
		return resp.Status
	}

	// VerifyVoteExtension
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, voteExt("val1", epoch, good("val1")))
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, voteExt("val1", epoch, forged))
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, voteExt("val1", epoch, good("val2")), "someone else's share")
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, voteExt("val1", 6, tle.DecryptionShare(f.secrets["val1"], 6)), "current epoch is open")
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, voteExt("val1", 4, tle.DecryptionShare(f.secrets["val1"], 4)), "no demand for epoch 4")

	// ProcessProposal (the SDK default handler stands in as `next`)
	acceptAll := func(sdk.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
	}
	process := ProcessProposalHandler(f.k, acceptAll)
	encode := func(d *types.InjectedDKGData) [][]byte {
		bz, err := EncodeDKGInjection(d)
		require.NoError(t, err)
		return [][]byte{bz}
	}
	resp, err := process(f.ctx, &abcitypes.RequestProcessProposal{Txs: encode(f.injected(epoch, map[string][]byte{"val1": good("val1"), "val2": forged}))})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, resp.Status, "a proposal carrying a bad share is rejected")
	resp, err = process(f.ctx, &abcitypes.RequestProcessProposal{Txs: encode(f.injected(epoch, map[string][]byte{"val1": good("val1"), "val2": good("val2")}))})
	require.NoError(t, err)
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, resp.Status)

	// PreBlocker: a forged share is dropped and announced; two good ones are
	// below the threshold of three.
	ct, err := tle.Encrypt(f.ks.MasterPublicKeyG2, epoch, []byte("sealed ballot"))
	require.NoError(t, err)
	req := &abcitypes.RequestFinalizeBlock{Txs: encode(f.injected(epoch, map[string][]byte{"val1": good("val1"), "val2": good("val2"), "val3": forged}))}
	ProcessDKGInjection(f.ctx, f.k, req)
	require.Len(t, req.Txs, 1, "injection stays in the block (one tx result per tx)")
	require.Equal(t, uint32(2), f.k.CountDecryptionShares(f.ctx, epoch))
	_, found := f.k.GetShieldEpochDecryptionKeyVal(f.ctx, epoch)
	require.False(t, found)
	var invalid int
	for _, ev := range f.ctx.EventManager().Events() {
		if ev.Type == types.EventTypeTLEInvalidShare {
			invalid++
			for _, a := range ev.Attributes {
				if a.Key == types.AttributeKeyValidator {
					require.Equal(t, "val3", a.Value)
				}
			}
		}
	}
	require.Equal(t, 1, invalid)

	// Third honest share: key reconstructed, ciphertext opens.
	req = &abcitypes.RequestFinalizeBlock{Txs: encode(f.injected(epoch, map[string][]byte{"val4": good("val4")}))}
	ProcessDKGInjection(f.ctx, f.k, req)
	key, found := f.k.GetShieldEpochDecryptionKeyVal(f.ctx, epoch)
	require.True(t, found)
	pt, err := tle.Decrypt(key.DecryptionKey, epoch, ct)
	require.NoError(t, err)
	require.Equal(t, []byte("sealed ballot"), pt)

	// Unknown consensus address: ignored, not fatal.
	req = &abcitypes.RequestFinalizeBlock{Txs: encode(&types.InjectedDKGData{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE,
		DecryptionShares: []*types.InjectedDecryptionShare{{ValidatorAddress: []byte("nobody-nobody-nobody"), Epoch: epoch, Share: good("val1")}}})}
	ProcessDKGInjection(f.ctx, f.k, req)
	require.Equal(t, uint32(3), f.k.CountDecryptionShares(f.ctx, epoch))
}

// TestExtendVoteServesDemandedEpochsOnly: the builder's epoch choice comes
// from the keeper (NextEpochNeedingShare), so with no demand there is
// nothing to embed even when ops are pending.
func TestExtendVoteEpochSelection(t *testing.T) {
	f := newShareFixture(t, []string{"val1", "val2"}, 2)
	require.NoError(t, f.k.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 3, EpochStartHeight: 30}))
	require.NoError(t, f.k.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 2, SubmittedAtEpoch: 2, SubmissionNullifier: []byte{1}}))

	_, ok := f.k.NextEpochNeedingShare(f.ctx, "val1")
	require.False(t, ok, "pending op alone does not release a share")

	require.NoError(t, f.k.SetDecryptionDemand(f.ctx, 2, 30))
	epoch, ok := f.k.NextEpochNeedingShare(f.ctx, "val1")
	require.True(t, ok)
	require.Equal(t, uint64(2), epoch)

	// Once this validator's share is stored, nothing more to do.
	_, err := f.k.StoreVerifiedDecryptionShare(f.ctx, "val1", 2, tle.DecryptionShare(f.secrets["val1"], 2))
	require.NoError(t, err)
	_, ok = f.k.NextEpochNeedingShare(f.ctx, "val1")
	require.False(t, ok)
	_, ok = f.k.NextEpochNeedingShare(f.ctx, "val2")
	require.True(t, ok)
}

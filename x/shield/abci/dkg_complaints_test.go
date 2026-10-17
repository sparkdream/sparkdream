package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// ceremonyFixture runs a full DKG round through the real vote-extension
// builders and the PreBlocker: three validators with their own key stores,
// one of which deals a bad evaluation.
type ceremonyFixture struct {
	*shareFixture
	vals     []string
	handlers map[string]*DKGVoteExtensionHandler
	state    types.DKGState
}

func newCeremonyFixture(t *testing.T) *ceremonyFixture {
	t.Helper()
	vals := []string{"val1", "val2", "val3"}
	f := newShareFixture(t, vals, 2)
	// The share fixture seeded an ACTIVE key set; this test runs the ceremony
	// that produces one, so start over at CONTRIBUTING with no key set.
	require.NoError(t, f.k.TLEKeySet.Remove(f.ctx))
	state := types.DKGState{
		Round:                 1,
		Phase:                 types.DKGPhase_DKG_PHASE_REGISTERING,
		RegistrationDeadline:  10,
		ContributionDeadline:  20,
		ComplaintDeadline:     30,
		JustificationDeadline: 40,
		ThresholdNumerator:    2,
		ThresholdDenominator:  3,
		ExpectedValidators:    vals,
	}
	require.NoError(t, f.k.SetDKGStateVal(f.ctx, state))

	c := &ceremonyFixture{shareFixture: f, vals: vals, handlers: map[string]*DKGVoteExtensionHandler{}, state: state}
	for _, v := range vals {
		c.handlers[v] = &DKGVoteExtensionHandler{
			keeper:   f.k,
			keyStore: keeper.NewDKGLocalKeyStore(t.TempDir()),
			consAddr: f.cons[v],
		}
	}
	return c
}

func (c *ceremonyFixture) setPhase(t *testing.T, phase types.DKGPhase) {
	t.Helper()
	st, _ := c.k.GetDKGStateVal(c.ctx)
	st.Phase = phase
	require.NoError(t, c.k.SetDKGStateVal(c.ctx, st))
	c.state = st
}

// extend runs a validator's ExtendVote and returns the decoded extension.
func (c *ceremonyFixture) extend(t *testing.T, v string) *types.DKGVoteExtension {
	t.Helper()
	resp, err := c.handlers[v].ExtendVoteHandler()(c.ctx, &abcitypes.RequestExtendVote{})
	require.NoError(t, err)
	if len(resp.VoteExtension) == 0 {
		return nil
	}
	var ext types.DKGVoteExtension
	require.NoError(t, ext.Unmarshal(resp.VoteExtension))
	return &ext
}

// verify runs VerifyVoteExtension for an extension claimed by validator v.
func (c *ceremonyFixture) verify(t *testing.T, v string, ext *types.DKGVoteExtension) abcitypes.ResponseVerifyVoteExtension_VerifyStatus {
	t.Helper()
	bz, err := ext.Marshal()
	require.NoError(t, err)
	resp, err := c.handlers[v].VerifyVoteExtensionHandler()(c.ctx, &abcitypes.RequestVerifyVoteExtension{VoteExtension: bz, ValidatorAddress: c.cons[v]})
	require.NoError(t, err)
	return resp.Status
}

// inject runs ProcessProposal and the PreBlocker on the given extensions.
func (c *ceremonyFixture) inject(t *testing.T, exts map[string]*types.DKGVoteExtension) abcitypes.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	data := &types.InjectedDKGData{Round: c.state.Round, Phase: c.state.Phase}
	for _, v := range c.vals { // deterministic order
		if ext, ok := exts[v]; ok && ext != nil {
			data.Extensions = append(data.Extensions, &types.ValidatorDKGExtension{ValidatorAddress: c.cons[v], Extension: *ext})
		}
	}
	bz, err := EncodeDKGInjection(data)
	require.NoError(t, err)
	acceptAll := func(sdk.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
	}
	resp, err := ProcessProposalHandler(c.k, acceptAll)(c.ctx, &abcitypes.RequestProcessProposal{Txs: [][]byte{bz}})
	require.NoError(t, err)
	if resp.Status == abcitypes.ResponseProcessProposal_ACCEPT {
		ProcessDKGInjection(c.ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{bz}})
	}
	return resp.Status
}

// TestDKGComplaintPipeline: a dealer that ships a garbage evaluation to one
// validator is complained about through the vote extensions; its valid
// justification repairs the victim's share, and the honest validators'
// shares reconstruct the master key. A forged justification is refused.
func TestDKGComplaintPipeline(t *testing.T) {
	c := newCeremonyFixture(t)

	// REGISTERING: everyone registers a key (PoP bound to operator + round).
	regs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		regs[v] = c.extend(t, v)
		require.NotNil(t, regs[v])
		require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, c.verify(t, v, regs[v]))
		// A registration replayed under another validator's identity fails its PoP.
		other := c.vals[(indexOf(c.vals, v)+1)%len(c.vals)]
		require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, c.verify(t, other, regs[v]))
	}
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, regs))
	for _, v := range c.vals {
		reg, found := c.k.GetDKGRegistration(c.ctx, v)
		require.True(t, found)
		require.Equal(t, uint64(1), reg.Round)
	}

	// CONTRIBUTING: val3 deals a garbage evaluation to val1.
	c.setPhase(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING)
	contribs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		contribs[v] = c.extend(t, v)
		require.NotNil(t, contribs[v])
		require.Len(t, contribs[v].EncryptedEvaluations, 2)
	}
	for _, e := range contribs["val3"].EncryptedEvaluations {
		if e.TargetIndex == 1 {
			e.Ciphertext = []byte("garbage that does not decrypt")
		}
	}
	for _, v := range c.vals {
		require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, c.verify(t, v, contribs[v]), "a bad evaluation is not detectable on chain")
	}
	// A contribution with a mismatched G2 commitment is refused.
	bad := *contribs["val2"]
	bad.FeldmanCommitmentsG2 = [][]byte{contribs["val1"].FeldmanCommitmentsG2[0], contribs["val2"].FeldmanCommitmentsG2[1]}
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, c.verify(t, "val2", &bad))
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, contribs))
	require.Len(t, c.k.GetAllDKGContributions(c.ctx), 3)

	// COMPLAINING: only val1 has something to say, and only about val3.
	c.setPhase(t, types.DKGPhase_DKG_PHASE_COMPLAINING)
	complaint := c.extend(t, "val1")
	require.NotNil(t, complaint)
	require.Equal(t, []uint32{3}, complaint.Complaints)
	require.Nil(t, c.extend(t, "val2"))
	require.Nil(t, c.extend(t, "val3"))
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, c.verify(t, "val1", complaint))
	self := &types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_COMPLAINING, Complaints: []uint32{1}}
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, c.verify(t, "val1", self), "no self-complaints")
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, map[string]*types.DKGVoteExtension{"val1": complaint}))
	rec, found := c.k.GetDKGComplaint(c.ctx, "val3", "val1")
	require.True(t, found)
	require.False(t, rec.Justified)
	requireDKGEvent(t, c.ctx, types.EventTypeShieldDKGComplaint, "val3", "val1")
	require.Nil(t, c.extend(t, "val1"), "a recorded complaint is not re-sent")

	// JUSTIFYING: a forged answer is refused; val3's real answer is recorded.
	c.setPhase(t, types.DKGPhase_DKG_PHASE_JUSTIFYING)
	require.Nil(t, c.extend(t, "val2"), "nothing to justify")
	just := c.extend(t, "val3")
	require.NotNil(t, just)
	require.Len(t, just.Justifications, 1)
	require.Equal(t, uint32(1), just.Justifications[0].ComplainerIndex)
	forged := &types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_JUSTIFYING,
		Justifications: []*types.DKGJustification{{ComplainerIndex: 1, Evaluation: make([]byte, 32)}}}
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, c.verify(t, "val3", forged))
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, c.inject(t, map[string]*types.DKGVoteExtension{"val3": forged}))
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_ACCEPT, c.verify(t, "val3", just))
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, map[string]*types.DKGVoteExtension{"val3": just}))
	rec, _ = c.k.GetDKGComplaint(c.ctx, "val3", "val1")
	require.True(t, rec.Justified)
	requireDKGEvent(t, c.ctx, types.EventTypeShieldDKGJustification, "val3", "val1")
	require.NoError(t, keeper.VerifyEvaluation(contribs["val3"].FeldmanCommitments, 1, rec.Evaluation))

	// Deadline: nobody is disqualified, the key set is aggregated.
	require.NoError(t, c.k.BeginBlocker(c.ctx.WithBlockHeight(40)))
	st, _ := c.k.GetDKGStateVal(c.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, st.Phase)
	require.Empty(t, st.DisqualifiedDealers)
	ks, found := c.k.GetTLEKeySetVal(c.ctx)
	require.True(t, found)
	require.NoError(t, ks.Validate())
	c.state = st

	// ACTIVE: every validator, including the repaired val1, serves a share
	// that passes the pairing check; two of them reconstruct a key that
	// opens a client's ciphertext.
	params, err := c.k.Params.Get(c.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.MaxPendingEpochs = 6
	require.NoError(t, c.k.Params.Set(c.ctx, params))
	const epoch = uint64(5)
	require.NoError(t, c.k.SetShieldEpochStateVal(c.ctx, types.ShieldEpochState{CurrentEpoch: 6, EpochStartHeight: 60}))
	require.NoError(t, c.k.SetDecryptionDemand(c.ctx, epoch, 60))
	ct, err := tle.Encrypt(ks.MasterPublicKeyG2, epoch, []byte("sealed ballot"))
	require.NoError(t, err)

	shares := map[string][]byte{}
	for _, v := range c.vals {
		ext := c.extend(t, v)
		require.NotNil(t, ext, "%s produced no share", v)
		require.Equal(t, epoch, ext.DecryptionEpoch)
		require.NoError(t, c.k.VerifyValidatorDecryptionShare(c.ctx, v, epoch, ext.DecryptionShare), "%s's share", v)
		shares[v] = ext.DecryptionShare
	}
	data := c.injected(epoch, map[string][]byte{"val1": shares["val1"], "val3": shares["val3"]})
	bz, err := EncodeDKGInjection(data)
	require.NoError(t, err)
	ProcessDKGInjection(c.ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{bz}})
	key, found := c.k.GetShieldEpochDecryptionKeyVal(c.ctx, epoch)
	require.True(t, found)
	pt, err := tle.Decrypt(key.DecryptionKey, epoch, ct)
	require.NoError(t, err)
	require.Equal(t, []byte("sealed ballot"), pt)

	// Without the justification val1 could not have served: its share would
	// not match its public share.
	var incoming []keeper.IncomingEvaluation
	for _, con := range c.k.GetAllDKGContributions(c.ctx) {
		if con.ValidatorAddress == "val1" {
			continue
		}
		ev := keeper.IncomingEvaluation{Dealer: con.ValidatorAddress, DealerIndex: valDKGIndex(st, con.ValidatorAddress), Commitments: con.FeldmanCommitments}
		for _, e := range con.EncryptedEvaluations {
			if e.TargetIndex == 1 {
				ev.Ciphertext = e.Ciphertext
			}
		}
		incoming = append(incoming, ev)
	}
	_, err = c.handlers["val1"].keyStore.ComputeDecryptionShare(1, epoch, 1, incoming)
	require.ErrorContains(t, err, "val3")
}

// TestDKGUnansweredComplaintDisqualifies: a dealer that ignores a complaint
// is left out of the key set, and the remaining dealers' shares still work.
func TestDKGUnansweredComplaintDisqualifies(t *testing.T) {
	c := newCeremonyFixture(t)
	regs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		regs[v] = c.extend(t, v)
	}
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, regs))
	c.setPhase(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING)
	contribs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		contribs[v] = c.extend(t, v)
	}
	// val3 simply omits val2's evaluation.
	var kept []*types.EncryptedEvaluation
	for _, e := range contribs["val3"].EncryptedEvaluations {
		if e.TargetIndex != 2 {
			kept = append(kept, e)
		}
	}
	contribs["val3"].EncryptedEvaluations = kept
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, contribs))

	c.setPhase(t, types.DKGPhase_DKG_PHASE_COMPLAINING)
	complaint := c.extend(t, "val2")
	require.NotNil(t, complaint)
	require.Equal(t, []uint32{3}, complaint.Complaints)
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, map[string]*types.DKGVoteExtension{"val2": complaint}))

	c.setPhase(t, types.DKGPhase_DKG_PHASE_JUSTIFYING)
	// val3 stays silent.
	require.NoError(t, c.k.BeginBlocker(c.ctx.WithBlockHeight(40)))
	st, _ := c.k.GetDKGStateVal(c.ctx)
	require.Equal(t, types.DKGPhase_DKG_PHASE_ACTIVE, st.Phase)
	require.Equal(t, []string{"val3"}, st.DisqualifiedDealers)
	ks, _ := c.k.GetTLEKeySetVal(c.ctx)
	c.state = st

	params, err := c.k.Params.Get(c.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, c.k.Params.Set(c.ctx, params))
	const epoch = uint64(3)
	require.NoError(t, c.k.SetShieldEpochStateVal(c.ctx, types.ShieldEpochState{CurrentEpoch: 4, EpochStartHeight: 40}))
	require.NoError(t, c.k.SetDecryptionDemand(c.ctx, epoch, 40))
	ct, err := tle.Encrypt(ks.MasterPublicKeyG2, epoch, []byte("x"))
	require.NoError(t, err)
	// val1 and val2 ignore the disqualified dealer and serve consistent shares.
	for _, v := range []string{"val1", "val2"} {
		ext := c.extend(t, v)
		require.NotNil(t, ext)
		stored, err := c.k.StoreVerifiedDecryptionShare(c.ctx, v, epoch, ext.DecryptionShare)
		require.NoError(t, err)
		require.True(t, stored)
		require.NoError(t, c.k.VerifyValidatorDecryptionShare(c.ctx, v, epoch, ext.DecryptionShare))
	}
	c.k.TryReconstructEpochKey(c.ctx, epoch)
	key, found := c.k.GetShieldEpochDecryptionKeyVal(c.ctx, epoch)
	require.True(t, found)
	_, err = tle.Decrypt(key.DecryptionKey, epoch, ct)
	require.NoError(t, err)
}

// requireDKGEvent asserts the context emitted an event of evType naming the
// dealer and complainer, in round 1.
func requireDKGEvent(t *testing.T, ctx sdk.Context, evType, dealer, complainer string) {
	t.Helper()
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != evType {
			continue
		}
		attrs := map[string]string{}
		for _, a := range ev.Attributes {
			attrs[a.Key] = a.Value
		}
		if attrs[types.AttributeKeyDealer] == dealer && attrs[types.AttributeKeyComplainer] == complainer {
			require.Equal(t, "1", attrs[types.AttributeKeyDKGRound])
			return
		}
	}
	t.Fatalf("no %s event for dealer %s, complainer %s", evType, dealer, complainer)
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

var _ kyber.Scalar = tle.Suite.G1().Scalar()

package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/types"
)

// recordingNext stands in for the SDK's default PrepareProposal handler: it
// records the request it was handed and returns two fixed mempool txs.
type recordingNext struct {
	calls int
	req   abcitypes.RequestPrepareProposal
	txs   [][]byte
}

func newRecordingNext() *recordingNext {
	return &recordingNext{txs: [][]byte{[]byte("mempool-tx-1"), []byte("mempool-tx-2")}}
}

func (r *recordingNext) handler() sdk.PrepareProposalHandler {
	return func(_ sdk.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
		r.calls++
		r.req = *req
		return &abcitypes.ResponsePrepareProposal{Txs: r.txs}, nil
	}
}

// commitVote wraps raw extension bytes in a committed vote from cons.
func commitVote(cons, ext []byte) abcitypes.ExtendedVoteInfo {
	return abcitypes.ExtendedVoteInfo{
		Validator:     abcitypes.Validator{Address: cons, Power: 1},
		VoteExtension: ext,
		BlockIdFlag:   cmtproto.BlockIDFlagCommit,
	}
}

func mustMarshalExt(t *testing.T, ext *types.DKGVoteExtension) []byte {
	t.Helper()
	bz, err := ext.Marshal()
	require.NoError(t, err)
	return bz
}

// safeMarshalExt marshals ext, or returns nil for a deliberately malformed
// extension the encoder can't handle (e.g. a nil evaluation entry).
func safeMarshalExt(ext *types.DKGVoteExtension) (bz []byte) {
	defer func() {
		if recover() != nil {
			bz = nil
		}
	}()
	bz, _ = ext.Marshal()
	return bz
}

func prepareReq(maxTxBytes int64, votes ...abcitypes.ExtendedVoteInfo) *abcitypes.RequestPrepareProposal {
	return &abcitypes.RequestPrepareProposal{
		MaxTxBytes:      maxTxBytes,
		LocalLastCommit: abcitypes.ExtendedCommitInfo{Round: 0, Votes: votes},
	}
}

// processAccepts runs ProcessProposal over a proposer's txs with a `next`
// that checks it receives everything after the injection.
func processAccepts(t *testing.T, c *shareFixture, txs [][]byte) abcitypes.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	next := func(_ sdk.Context, req *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
		require.Equal(t, txs[1:], req.Txs, "next sees the block without the injection")
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
	}
	resp, err := ProcessProposalHandler(c.k, next)(c.ctx, &abcitypes.RequestProcessProposal{Txs: txs})
	require.NoError(t, err)
	return resp.Status
}

// registrations collects every fixture validator's REGISTERING extension.
func (c *ceremonyFixture) registrations(t *testing.T) map[string]*types.DKGVoteExtension {
	t.Helper()
	regs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		regs[v] = c.extend(t, v)
		require.NotNil(t, regs[v])
	}
	return regs
}

// TestPrepareProposalFallsThrough: with nothing to inject the block is
// exactly what `next` builds, with the full byte budget.
func TestPrepareProposalFallsThrough(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, c *ceremonyFixture)
	}{
		{"no DKG state", func(t *testing.T, c *ceremonyFixture) {
			require.NoError(t, c.k.DKGState.Remove(c.ctx))
		}},
		{"INACTIVE phase", func(t *testing.T, c *ceremonyFixture) {
			c.setPhase(t, types.DKGPhase_DKG_PHASE_INACTIVE)
		}},
		{"shield disabled during REGISTERING", func(t *testing.T, c *ceremonyFixture) {
			params, err := c.k.Params.Get(c.ctx)
			require.NoError(t, err)
			params.Enabled = false
			require.NoError(t, c.k.Params.Set(c.ctx, params))
		}},
		{"no vote carries an extension", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCeremonyFixture(t)
			var votes []abcitypes.ExtendedVoteInfo
			if tc.setup != nil {
				// Valid registrations are on offer; the handler must ignore them.
				for v, ext := range c.registrations(t) {
					votes = append(votes, c.vote(v, mustMarshalExt(t, ext)))
				}
				tc.setup(t, c)
			} else {
				for _, v := range c.vals {
					votes = append(votes, c.vote(v, nil))
				}
			}
			rec := newRecordingNext()
			resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(1<<20, votes...))
			require.NoError(t, err)
			require.Equal(t, 1, rec.calls)
			require.Equal(t, int64(1<<20), rec.req.MaxTxBytes, "full budget passed through")
			require.Equal(t, rec.txs, resp.Txs)
			for _, tx := range resp.Txs {
				require.False(t, HasDKGInjectionPrefix(tx))
			}
		})
	}
}

// TestPrepareProposalInjectsValidExtensions: valid REGISTERING extensions
// land in one injection at Txs[0]; malformed, unknown-validator,
// non-participant and PoP-failing ones are left out; `next` gets the budget
// minus the injection; ProcessProposal accepts the block.
func TestPrepareProposalInjectsValidExtensions(t *testing.T) {
	c := newCeremonyFixture(t)
	regs := c.registrations(t)

	// A fourth validator known to staking but not in the DKG.
	outsider := []byte("outsider-cons-addr-0")
	byCons := map[string]string{string(outsider): "val9"}
	for _, v := range c.vals {
		byCons[string(c.cons[v])] = v
	}
	c.k.SetStakingKeeper(stakingStub{byCons: byCons, pubs: c.pubs})

	const maxTxBytes = int64(1 << 20)
	votes := []abcitypes.ExtendedVoteInfo{
		c.vote("val1", mustMarshalExt(t, regs["val1"])),
		c.vote("val2", mustMarshalExt(t, regs["val2"])),
		c.vote("val3", mustMarshalExt(t, regs["val1"])),                             // val1's PoP under val3: invalid
		commitVote([]byte("nobody-nobody-nobody"), mustMarshalExt(t, regs["val3"])), // unknown cons addr
		commitVote(outsider, mustMarshalExt(t, regs["val3"])),                       // not a participant
		c.vote("val3", []byte{0xff, 0xff, 0xff}),                                    // malformed
		c.vote("val3", nil),                                                         // no extension
	}
	rec := newRecordingNext()
	resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(maxTxBytes, votes...))
	require.NoError(t, err)
	require.Equal(t, 1, rec.calls)
	require.Len(t, resp.Txs, 1+len(rec.txs))
	require.True(t, HasDKGInjectionPrefix(resp.Txs[0]), "injection is tx 0")
	require.Equal(t, rec.txs, resp.Txs[1:])
	require.Equal(t, maxTxBytes-int64(len(resp.Txs[0])), rec.req.MaxTxBytes, "next gets what the injection leaves")

	injected, err := DecodeDKGInjection(resp.Txs[0])
	require.NoError(t, err)
	require.Equal(t, c.state.Round, injected.Round)
	require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, injected.Phase)
	require.Empty(t, injected.DecryptionShares)
	require.Len(t, injected.Extensions, 2)
	require.Equal(t, c.cons["val1"], injected.Extensions[0].ValidatorAddress)
	require.Equal(t, *regs["val1"], injected.Extensions[0].Extension)
	require.Equal(t, c.cons["val2"], injected.Extensions[1].ValidatorAddress)
	require.Equal(t, *regs["val2"], injected.Extensions[1].Extension)

	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, processAccepts(t, c.shareFixture, resp.Txs))
}

// TestPrepareProposalInjectsDecryptionShares: in ACTIVE, honest shares for a
// demanded epoch are injected as InjectedDecryptionShares; a forged one is
// left out, and ProcessProposal accepts the block.
func TestPrepareProposalInjectsDecryptionShares(t *testing.T) {
	vals := []string{"val1", "val2", "val3"}
	f := newShareFixture(t, vals, 2)
	require.NoError(t, f.k.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 6, EpochStartHeight: 60}))
	const epoch = uint64(5)
	require.NoError(t, f.k.SetDecryptionDemand(f.ctx, epoch, 60))
	forged, _ := tle.Suite.G1().Point().Pick(random.New()).MarshalBinary()

	shareExt := func(sh []byte) []byte {
		return mustMarshalExt(t, &types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE, DecryptionEpoch: epoch, DecryptionShare: sh})
	}
	votes := []abcitypes.ExtendedVoteInfo{
		f.vote("val1", shareExt(tle.DecryptionShare(f.secrets["val1"], epoch))),
		f.vote("val2", shareExt(forged)),
		f.vote("val3", shareExt(tle.DecryptionShare(f.secrets["val3"], epoch))),
	}
	rec := newRecordingNext()
	resp, err := PrepareProposalHandler(f.k, rec.handler())(f.ctx, prepareReq(1<<20, votes...))
	require.NoError(t, err)
	require.True(t, HasDKGInjectionPrefix(resp.Txs[0]))
	require.Equal(t, int64(1<<20)-int64(len(resp.Txs[0])), rec.req.MaxTxBytes)

	injected, err := DecodeDKGInjection(resp.Txs[0])
	require.NoError(t, err)
	require.Empty(t, injected.Extensions)
	require.Len(t, injected.DecryptionShares, 2)
	for i, v := range []string{"val1", "val3"} {
		require.Equal(t, f.cons[v], injected.DecryptionShares[i].ValidatorAddress)
		require.Equal(t, epoch, injected.DecryptionShares[i].Epoch)
		require.Equal(t, tle.DecryptionShare(f.secrets[v], epoch), injected.DecryptionShares[i].Share)
	}
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, processAccepts(t, f, resp.Txs))
}

// TestPrepareProposalInjectionBudget: the injection is capped at half of
// MaxTxBytes; extensions past the cap are deferred, and with room for none
// the block falls through to `next` unchanged.
func TestPrepareProposalInjectionBudget(t *testing.T) {
	c := newCeremonyFixture(t)
	regs := c.registrations(t)
	var votes []abcitypes.ExtendedVoteInfo
	for _, v := range c.vals {
		votes = append(votes, c.vote(v, mustMarshalExt(t, regs[v])))
	}

	// Mirror the handler's accounting: header allowance + (item size + 8) each.
	header := int64(len(DKGInjectionPrefix)) + 16
	itemSize := func(v string) int64 {
		ve := c.signedExt(v, regs[v])
		return int64(ve.Size()) + 8
	}

	t.Run("room for one", func(t *testing.T) {
		// Budget fits val1 exactly; val2 (same shape) and val3 are deferred.
		require.Equal(t, itemSize("val1"), itemSize("val2"))
		maxTxBytes := injectionBudgetDivisor * (header + itemSize("val1"))
		rec := newRecordingNext()
		resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(maxTxBytes, votes...))
		require.NoError(t, err)
		require.True(t, HasDKGInjectionPrefix(resp.Txs[0]))
		injected, err := DecodeDKGInjection(resp.Txs[0])
		require.NoError(t, err)
		require.Len(t, injected.Extensions, 1)
		require.Equal(t, c.cons["val1"], injected.Extensions[0].ValidatorAddress)
		require.LessOrEqual(t, int64(len(resp.Txs[0])), maxTxBytes/injectionBudgetDivisor)
		require.Equal(t, maxTxBytes-int64(len(resp.Txs[0])), rec.req.MaxTxBytes)
	})

	t.Run("room for two", func(t *testing.T) {
		maxTxBytes := injectionBudgetDivisor * (header + itemSize("val1") + itemSize("val2"))
		rec := newRecordingNext()
		resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(maxTxBytes, votes...))
		require.NoError(t, err)
		injected, err := DecodeDKGInjection(resp.Txs[0])
		require.NoError(t, err)
		require.Len(t, injected.Extensions, 2)
	})

	t.Run("room for none", func(t *testing.T) {
		maxTxBytes := injectionBudgetDivisor * (header + itemSize("val1") - 1)
		rec := newRecordingNext()
		resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(maxTxBytes, votes...))
		require.NoError(t, err)
		require.Equal(t, rec.txs, resp.Txs, "nothing fits: block is next's")
		require.Equal(t, maxTxBytes, rec.req.MaxTxBytes)
	})
}

// --- acceptance-rule rejections ---

type extCase struct {
	name   string
	op     string // operator the extension is attributed to
	ext    func() *types.DKGVoteExtension
	errSub string
}

// checkExtCases asserts the base extension passes and every mutation is
// refused both by validateExtension and, wrapped in an injection, by
// validateInjectedDKGData.
func checkExtCases(t *testing.T, c *ceremonyFixture, baseOp string, base *types.DKGVoteExtension, cases []extCase) {
	t.Helper()
	st, found := c.k.GetDKGStateVal(c.ctx)
	require.True(t, found)
	require.NoError(t, validateExtension(c.ctx, c.k, base, st, baseOp), "base extension is valid")
	require.NoError(t, validateInjected(c.ctx, c.k, &types.InjectedDKGData{
		Round: st.Round, Phase: st.Phase,
		Extensions: []*types.ValidatorDKGExtension{c.signedExt(baseOp, base)},
	}))
	for _, tc := range cases {
		t.Run(st.Phase.String()+"/"+tc.name, func(t *testing.T) {
			op := tc.op
			if op == "" {
				op = baseOp
			}
			ext := tc.ext()
			err := validateExtension(c.ctx, c.k, ext, st, op)
			require.ErrorContains(t, err, tc.errSub)
			cons, ok := c.cons[op]
			if !ok {
				return // no consensus address to inject under
			}
			bad := &types.ValidatorDKGExtension{ValidatorAddress: cons, Extension: *ext, ExtensionSignature: c.sign(op, safeMarshalExt(ext))}
			if op == baseOp {
				// One extension per validator: inject the bad one alone.
				err = validateInjected(c.ctx, c.k, &types.InjectedDKGData{
					Round: st.Round, Phase: st.Phase, Extensions: []*types.ValidatorDKGExtension{bad},
				})
				require.ErrorContains(t, err, "extension 0")
				require.ErrorContains(t, err, tc.errSub)
				return
			}
			err = validateInjected(c.ctx, c.k, &types.InjectedDKGData{
				Round: st.Round, Phase: st.Phase,
				Extensions: []*types.ValidatorDKGExtension{c.signedExt(baseOp, base), bad},
			})
			require.ErrorContains(t, err, "extension 1")
			require.ErrorContains(t, err, tc.errSub)
		})
	}
}

func cloneEvals(evals []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
	out := make([]*types.EncryptedEvaluation, len(evals))
	for i, e := range evals {
		cp := *e
		out[i] = &cp
	}
	return out
}

// TestValidateExtensionRejections walks a ceremony (val2 never contributes,
// val3 deals val1 a garbage evaluation) and at each phase checks the
// acceptance rule refuses every malformed variant of a real extension.
func TestValidateExtensionRejections(t *testing.T) {
	c := newCeremonyFixture(t)

	// REGISTERING
	regs := c.registrations(t)
	identity, err := tleSuite.G1().Point().Null().MarshalBinary()
	require.NoError(t, err)
	mutReg := func(mut func(e *types.DKGVoteExtension)) func() *types.DKGVoteExtension {
		return func() *types.DKGVoteExtension {
			e := *regs["val1"]
			mut(&e)
			return &e
		}
	}
	checkExtCases(t, c, "val1", regs["val1"], []extCase{
		{name: "round mismatch", ext: mutReg(func(e *types.DKGVoteExtension) { e.Round = 2 }), errSub: "round mismatch"},
		{name: "phase mismatch", ext: mutReg(func(e *types.DKGVoteExtension) { e.Phase = types.DKGPhase_DKG_PHASE_CONTRIBUTING }), errSub: "phase mismatch"},
		{name: "non-participant", op: "val9", ext: mutReg(func(*types.DKGVoteExtension) {}), errSub: "not a DKG participant"},
		{name: "empty pub key", ext: mutReg(func(e *types.DKGVoteExtension) { e.RegistrationPubKey = nil }), errSub: "empty pub key"},
		{name: "garbage pub key", ext: mutReg(func(e *types.DKGVoteExtension) { e.RegistrationPubKey = []byte("not a point") }), errSub: "invalid pub key"},
		{name: "identity pub key", ext: mutReg(func(e *types.DKGVoteExtension) { e.RegistrationPubKey = identity }), errSub: "identity element"},
		{name: "someone else's registration", op: "val2", ext: mutReg(func(*types.DKGVoteExtension) {}), errSub: "registration PoP"},
	})
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, regs))

	// CONTRIBUTING
	c.setPhase(t, types.DKGPhase_DKG_PHASE_CONTRIBUTING)
	contribs := map[string]*types.DKGVoteExtension{}
	for _, v := range c.vals {
		contribs[v] = c.extend(t, v)
		require.NotNil(t, contribs[v])
	}
	for _, e := range contribs["val3"].EncryptedEvaluations {
		if e.TargetIndex == 1 {
			e.Ciphertext = []byte("garbage that does not decrypt")
		}
	}
	mutEvals := func(mut func(evals []*types.EncryptedEvaluation) []*types.EncryptedEvaluation) func() *types.DKGVoteExtension {
		return func() *types.DKGVoteExtension {
			e := *contribs["val1"]
			e.EncryptedEvaluations = mut(cloneEvals(e.EncryptedEvaluations))
			return &e
		}
	}
	checkExtCases(t, c, "val1", contribs["val1"], []extCase{
		{name: "target index 0", ext: mutEvals(func(ev []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
			ev[0].TargetIndex = 0
			return ev
		}), errSub: "invalid target index 0"},
		{name: "target index > n", ext: mutEvals(func(ev []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
			ev[0].TargetIndex = 4
			return ev
		}), errSub: "invalid target index 4"},
		{name: "duplicate target", ext: mutEvals(func(ev []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
			cp := *ev[0]
			return append(ev, &cp)
		}), errSub: "duplicate target index"},
		{name: "empty ciphertext", ext: mutEvals(func(ev []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
			ev[0].Ciphertext = nil
			return ev
		}), errSub: "empty ciphertext"},
		{name: "someone else's contribution", op: "val2", ext: mutEvals(func(ev []*types.EncryptedEvaluation) []*types.EncryptedEvaluation {
			return ev
		}), errSub: "contribution PoP"},
	})
	// val2 never contributes.
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, map[string]*types.DKGVoteExtension{"val1": contribs["val1"], "val3": contribs["val3"]}))
	_, found := c.k.GetDKGContributionVal(c.ctx, "val2")
	require.False(t, found)

	// COMPLAINING
	c.setPhase(t, types.DKGPhase_DKG_PHASE_COMPLAINING)
	complaint := c.extend(t, "val1")
	require.NotNil(t, complaint)
	require.Equal(t, []uint32{3}, complaint.Complaints)
	complain := func(idx ...uint32) func() *types.DKGVoteExtension {
		return func() *types.DKGVoteExtension {
			return &types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_COMPLAINING, Complaints: idx}
		}
	}
	checkExtCases(t, c, "val1", complaint, []extCase{
		{name: "dealer index 0", ext: complain(0), errSub: "invalid dealer index 0"},
		{name: "dealer index > n", ext: complain(4), errSub: "invalid dealer index 4"},
		{name: "self complaint", ext: complain(1), errSub: "complains about itself"},
		{name: "duplicate dealer", ext: complain(3, 3), errSub: "duplicate dealer index 3"},
		{name: "dealer without contribution", ext: complain(2), errSub: "has no contribution"},
	})
	require.Equal(t, abcitypes.ResponseProcessProposal_ACCEPT, c.inject(t, map[string]*types.DKGVoteExtension{"val1": complaint}))

	// JUSTIFYING
	c.setPhase(t, types.DKGPhase_DKG_PHASE_JUSTIFYING)
	just := c.extend(t, "val3")
	require.NotNil(t, just)
	require.Len(t, just.Justifications, 1)
	real := just.Justifications[0]
	justify := func(js ...*types.DKGJustification) func() *types.DKGVoteExtension {
		return func() *types.DKGVoteExtension {
			return &types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_JUSTIFYING, Justifications: js}
		}
	}
	checkExtCases(t, c, "val3", just, []extCase{
		{name: "nil justification", ext: justify(nil), errSub: "justification 0: empty"},
		{name: "complainer index 0", ext: justify(&types.DKGJustification{ComplainerIndex: 0, Evaluation: real.Evaluation}), errSub: "invalid complainer index 0"},
		{name: "complainer index > n", ext: justify(&types.DKGJustification{ComplainerIndex: 4, Evaluation: real.Evaluation}), errSub: "invalid complainer index 4"},
		{name: "duplicate complainer", ext: justify(real, real), errSub: "duplicate complainer index 1"},
		{name: "dealer without contribution", op: "val2", ext: justify(real), errSub: "has no contribution"},
		{name: "unfiled complaint", ext: justify(&types.DKGJustification{ComplainerIndex: 2, Evaluation: real.Evaluation}), errSub: "did not complain"},
		{name: "forged evaluation", ext: justify(&types.DKGJustification{ComplainerIndex: 1, Evaluation: make([]byte, 32)}), errSub: "justification 0"},
	})
}

// TestValidateActiveAndInjectionEnvelope: ACTIVE-phase extensions need a
// share, and validateInjectedDKGData refuses envelopes that do not match the
// state or name no known validator.
func TestValidateActiveAndInjectionEnvelope(t *testing.T) {
	vals := []string{"val1", "val2", "val3"}
	f := newShareFixture(t, vals, 2)
	require.NoError(t, f.k.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 6, EpochStartHeight: 60}))
	const epoch = uint64(5)
	require.NoError(t, f.k.SetDecryptionDemand(f.ctx, epoch, 60))
	st, _ := f.k.GetDKGStateVal(f.ctx)
	good := tle.DecryptionShare(f.secrets["val1"], epoch)

	base := types.DKGVoteExtension{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE, DecryptionEpoch: epoch, DecryptionShare: good}
	require.NoError(t, validateExtension(f.ctx, f.k, &base, st, "val1"))
	empty := base
	empty.DecryptionShare = nil
	require.ErrorContains(t, validateExtension(f.ctx, f.k, &empty, st, "val1"), "empty decryption share")
	require.ErrorContains(t, validateInjected(f.ctx, f.k, &types.InjectedDKGData{Round: 1, Phase: types.DKGPhase_DKG_PHASE_ACTIVE,
		Extensions: []*types.ValidatorDKGExtension{{ValidatorAddress: f.cons["val1"], Extension: empty}}}), "empty decryption share")

	share := func(cons []byte, sh []byte) *types.InjectedDecryptionShare {
		return &types.InjectedDecryptionShare{ValidatorAddress: cons, Epoch: epoch, Share: sh}
	}
	ext := func(cons []byte) *types.ValidatorDKGExtension {
		if string(cons) == string(f.cons["val1"]) {
			return f.signedExt("val1", &base)
		}
		return &types.ValidatorDKGExtension{ValidatorAddress: cons, Extension: base}
	}
	cases := []struct {
		name   string
		data   *types.InjectedDKGData
		errSub string
	}{
		{"round mismatch", &types.InjectedDKGData{Round: 2, Phase: st.Phase}, "round mismatch"},
		{"phase mismatch", &types.InjectedDKGData{Round: 1, Phase: types.DKGPhase_DKG_PHASE_REGISTERING}, "phase mismatch"},
		{"nil extension", &types.InjectedDKGData{Round: 1, Phase: st.Phase, Extensions: []*types.ValidatorDKGExtension{nil}}, "extension 0: empty validator address"},
		{"extension without validator", &types.InjectedDKGData{Round: 1, Phase: st.Phase, Extensions: []*types.ValidatorDKGExtension{ext(nil)}}, "extension 0: empty validator address"},
		{"extension from unknown cons addr", &types.InjectedDKGData{Round: 1, Phase: st.Phase, Extensions: []*types.ValidatorDKGExtension{ext([]byte("nobody-nobody-nobody"))}}, "validator not found"},
		{"nil share", &types.InjectedDKGData{Round: 1, Phase: st.Phase, DecryptionShares: []*types.InjectedDecryptionShare{nil}}, "decryption share 0: empty validator address"},
		{"share without validator", &types.InjectedDKGData{Round: 1, Phase: st.Phase, DecryptionShares: []*types.InjectedDecryptionShare{share(nil, good)}}, "decryption share 0: empty validator address"},
		{"empty share", &types.InjectedDKGData{Round: 1, Phase: st.Phase, DecryptionShares: []*types.InjectedDecryptionShare{share(f.cons["val1"], good), share(f.cons["val2"], nil)}}, "decryption share 1: empty share"},
		{"share from unknown cons addr", &types.InjectedDKGData{Round: 1, Phase: st.Phase, DecryptionShares: []*types.InjectedDecryptionShare{share([]byte("nobody-nobody-nobody"), good)}}, "validator not found"},
		{"share under another validator", &types.InjectedDKGData{Round: 1, Phase: st.Phase, DecryptionShares: []*types.InjectedDecryptionShare{share(f.cons["val2"], good)}}, "decryption share 0 from val2"},
	}
	require.NoError(t, validateInjected(f.ctx, f.k, &types.InjectedDKGData{Round: 1, Phase: st.Phase,
		Extensions:       []*types.ValidatorDKGExtension{ext(f.cons["val1"])},
		DecryptionShares: []*types.InjectedDecryptionShare{share(f.cons["val1"], good)}}), "valid envelope")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorContains(t, validateInjected(f.ctx, f.k, tc.data), tc.errSub)
		})
	}

	t.Run("no DKG state", func(t *testing.T) {
		require.NoError(t, f.k.DKGState.Remove(f.ctx))
		require.ErrorContains(t, validateInjected(f.ctx, f.k, &types.InjectedDKGData{Round: 1, Phase: st.Phase}), "no DKG state")
		// ProcessProposal rejects any injection then.
		bz, err := EncodeDKGInjection(&types.InjectedDKGData{Round: 1, Phase: st.Phase})
		require.NoError(t, err)
		require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, processStatus(t, f, [][]byte{bz}))
	})
}

func processStatus(t *testing.T, f *shareFixture, txs [][]byte) abcitypes.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	acceptAll := func(sdk.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
		return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
	}
	resp, err := ProcessProposalHandler(f.k, acceptAll)(f.ctx, &abcitypes.RequestProcessProposal{Txs: txs})
	require.NoError(t, err)
	return resp.Status
}

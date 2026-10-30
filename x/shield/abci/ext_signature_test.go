package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestInjectedExtensionsMustBeSignedByTheirValidator (SHD-02): a proposer can
// only inject ceremony data the named validator signed in the last commit.
// A registration (or contribution, complaint, justification) attributed to
// another validator but unsigned, signed by the proposer's own key, or signed
// for another height or round is refused by ProcessProposal and ignored by the
// PreBlocker, so a single proposer can't choose other validators' keys or
// polynomials, nor complain on their behalf against honest dealers.
func TestInjectedExtensionsMustBeSignedByTheirValidator(t *testing.T) {
	c := newCeremonyFixture(t)
	regs := c.registrations(t)
	victim, proposer := "val2", "val1"
	ext := regs[victim]
	bz := mustMarshalExt(t, ext)

	wrongHeight, err := extensionSignBytes(bz, testSignedHeight-1, testSignedRound, c.ctx.ChainID())
	require.NoError(t, err)
	sigWrongHeight, err := c.privs[victim].Sign(wrongHeight)
	require.NoError(t, err)
	wrongRound, err := extensionSignBytes(bz, testSignedHeight, testSignedRound+1, c.ctx.ChainID())
	require.NoError(t, err)
	sigWrongRound, err := c.privs[victim].Sign(wrongRound)
	require.NoError(t, err)

	forgeries := map[string][]byte{
		"unsigned":                  nil,
		"signed by the proposer":    c.sign(proposer, bz),
		"signed for another height": sigWrongHeight,
		"signed for another round":  sigWrongRound,
		"garbage signature":         make([]byte, 64),
	}
	for name, sig := range forgeries {
		t.Run(name, func(t *testing.T) {
			forged := &types.ValidatorDKGExtension{ValidatorAddress: c.cons[victim], Extension: *ext, ExtensionSignature: sig}
			data := &types.InjectedDKGData{Round: c.state.Round, Phase: c.state.Phase, Extensions: []*types.ValidatorDKGExtension{forged}}
			require.ErrorContains(t, validateInjected(c.ctx, c.k, data), "signature")

			injection, err := EncodeDKGInjection(data)
			require.NoError(t, err)
			require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, processStatus(t, c.shareFixture, [][]byte{injection}))

			// A block that slipped past ProcessProposal (or is replayed by a
			// syncing node) still doesn't store it.
			ProcessDKGInjection(c.ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{injection}})
			_, found := c.k.GetDKGRegistration(c.ctx, victim)
			require.False(t, found, "forged registration stored")
		})
	}

	// The victim's own signature is accepted and stored.
	data := &types.InjectedDKGData{Round: c.state.Round, Phase: c.state.Phase, Extensions: []*types.ValidatorDKGExtension{c.signedExt(victim, ext)}}
	require.NoError(t, validateInjected(c.ctx, c.k, data))
	injection, err := EncodeDKGInjection(data)
	require.NoError(t, err)
	ProcessDKGInjection(c.ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{injection}})
	_, found := c.k.GetDKGRegistration(c.ctx, victim)
	require.True(t, found)

	// PrepareProposal leaves out a vote whose signature doesn't verify.
	forgedVote := commitVote(c.cons["val3"], mustMarshalExt(t, regs["val3"]))
	forgedVote.ExtensionSignature = c.sign(proposer, mustMarshalExt(t, regs["val3"]))
	rec := newRecordingNext()
	resp, err := PrepareProposalHandler(c.k, rec.handler())(c.ctx, prepareReq(1<<20, forgedVote, c.vote(proposer, mustMarshalExt(t, regs[proposer]))))
	require.NoError(t, err)
	injected, err := DecodeDKGInjection(resp.Txs[0])
	require.NoError(t, err)
	require.Len(t, injected.Extensions, 1)
	require.Equal(t, c.cons[proposer], injected.Extensions[0].ValidatorAddress)
}

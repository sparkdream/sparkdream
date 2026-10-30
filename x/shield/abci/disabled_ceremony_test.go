package abci

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// TestDisabledShieldFreezesCeremony: with params.enabled false the
// BeginBlocker cannot advance a DKG phase, so no ABCI path produces, accepts
// or stores ceremony data for it (PrepareProposal shares the same gate). ACTIVE-phase shares are not frozen.
func TestDisabledShieldFreezesCeremony(t *testing.T) {
	c := newCeremonyFixture(t)
	v := c.vals[0]
	reg := c.extend(t, v)
	require.NotNil(t, reg, "enabled: registration produced")

	params, err := c.k.Params.Get(c.ctx)
	require.NoError(t, err)
	params.Enabled = false
	require.NoError(t, c.k.Params.Set(c.ctx, params))

	require.Nil(t, c.extend(t, v), "disabled: no registration produced")
	require.Equal(t, abcitypes.ResponseVerifyVoteExtension_REJECT, c.verify(t, v, reg))
	require.Equal(t, abcitypes.ResponseProcessProposal_REJECT, c.inject(t, map[string]*types.DKGVoteExtension{v: reg}))

	// The PreBlocker ignores an injection that slipped past ProcessProposal.
	bz, err := EncodeDKGInjection(&types.InjectedDKGData{
		Round: c.state.Round, Phase: c.state.Phase,
		Extensions: []*types.ValidatorDKGExtension{c.signedExt(v, reg)},
	})
	require.NoError(t, err)
	ProcessDKGInjection(c.ctx, c.k, &abcitypes.RequestFinalizeBlock{Txs: [][]byte{bz}})
	_, found := c.k.GetDKGRegistration(c.ctx, v)
	require.False(t, found, "disabled: registration not stored")

	require.False(t, ceremonyFrozen(c.ctx, c.k, types.DKGPhase_DKG_PHASE_ACTIVE), "ACTIVE shares keep flowing")
}

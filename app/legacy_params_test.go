package app

import (
	"testing"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramsproposal "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

// TestLegacyParamChangeProposalCannotReachGuardedModules pins GRD-201: x/params
// still registers its ParameterChangeProposal handler in gov's legacy router
// (reachable through MsgExecLegacyContent, outside guardian). The depinject
// wiring hands every SDK module a legacy subspace for migrations, but only the
// four IBC subspaces carry a key table (app/ibc.go). A legacy param change
// aimed at a guardian-filtered module therefore finds a subspace with no
// registered keys: Subspace.Update panics ("parameter ... not registered"),
// gov's safeExecuteHandler recovers it and the proposal fails. The route
// cannot be used to sidestep guardian's filters.
func TestLegacyParamChangeProposalCannotReachGuardedModules(t *testing.T) {
	app := testApp(t)
	ctx := app.BaseApp.NewContext(true)

	router := app.GovKeeper.LegacyRouter()
	require.NotNil(t, router)
	require.True(t, router.HasRoute(paramsproposal.RouterKey), "x/params gov handler is wired; if it ever goes away, GRD-201 is moot and this test can be dropped")
	handler := router.GetRoute(paramsproposal.RouterKey)

	for _, tc := range []struct {
		subspace, key, value string
	}{
		{stakingtypes.ModuleName, "UnbondingTime", `"1s"`},
		{minttypes.ModuleName, "InflationMax", `"0.9"`},
		{banktypes.ModuleName, "DefaultSendEnabled", `false`},
		{distrtypes.ModuleName, "CommunityTax", `"0"`},
	} {
		t.Run(tc.subspace, func(t *testing.T) {
			ss, found := app.ParamsKeeper.GetSubspace(tc.subspace)
			if found {
				require.False(t, ss.HasKeyTable(), "%s has a legacy key table; a ParameterChangeProposal could write it behind guardian", tc.subspace)
			}
			prop := paramsproposal.NewParameterChangeProposal("phoenix", "legacy change",
				[]paramsproposal.ParamChange{paramsproposal.NewParamChange(tc.subspace, tc.key, tc.value)})
			cacheCtx, _ := ctx.CacheContext()
			if !found {
				err := handler(cacheCtx, prop)
				require.ErrorIs(t, err, paramsproposal.ErrUnknownSubspace)
				return
			}
			require.PanicsWithValue(t, "parameter "+tc.key+" not registered", func() {
				_ = handler(cacheCtx, prop)
			}, "legacy param change for %s must not land", tc.subspace)
		})
	}

	// The IBC subspaces are the only ones with a key table: ibc-go v10 reads
	// its params from its own store, so even a write there is a no-op.
	ss, found := app.ParamsKeeper.GetSubspace(ibcexported.ModuleName)
	require.True(t, found)
	require.True(t, ss.HasKeyTable())
}

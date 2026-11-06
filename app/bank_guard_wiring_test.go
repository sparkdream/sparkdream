package app

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
)

// TestFederationBankKeeperIsGuarded pins that every first-party
// SetDenomMetaData writer goes through BankKeeperWithIdentityGuard:
// depinject hands modules the raw bank keeper, so a module that writes
// metadata must be re-wired with the wrapped keeper after Inject. Today the
// writers are x/identity (SetBankKeeper in app.go) and x/federation (peer
// identity ibc/... denoms).
func TestFederationBankKeeperIsGuarded(t *testing.T) {
	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	app := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID("guard-test"))
	_, ok := app.BankKeeper.(BankKeeperWithIdentityGuard)
	require.True(t, ok, "app.BankKeeper must be the identity guard")
	_, ok = app.FederationKeeper.BankKeeper().(BankKeeperWithIdentityGuard)
	require.True(t, ok, "x/federation must write DenomMetadata through the identity guard")
}

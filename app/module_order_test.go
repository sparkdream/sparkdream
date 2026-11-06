package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	federationmoduletypes "sparkdream/x/federation/types"
	repmoduletypes "sparkdream/x/rep/types"
	servicemoduletypes "sparkdream/x/service/types"
	shieldmoduletypes "sparkdream/x/shield/types"
	splitmoduletypes "sparkdream/x/split/types"
)

// indexOf returns the position of name in order, failing the test when it is
// missing (a module dropped from an ordering list runs in map order).
func indexOf(t *testing.T, list string, order []string, name string) int {
	t.Helper()
	for i, n := range order {
		if n == name {
			return i
		}
	}
	t.Fatalf("%s missing from %s %v", name, list, order)
	return -1
}

// TestModuleOrderPins pins the Begin/EndBlocker and InitGenesis orderings
// that other modules depend on (app_config.go):
//
//   - shield, rep and federation skim the community pool and must run before
//     split in BeginBlock, which distributes whatever remains in full;
//   - service runs before federation in EndBlock and InitGenesis, because
//     service's operator hooks suspend, unsuspend or prune federation's
//     BridgeBindings and federation rebuilds bindings against service's
//     operators at genesis.
func TestModuleOrderPins(t *testing.T) {
	app := testApp(t)
	begin := app.ModuleManager.OrderBeginBlockers
	end := app.ModuleManager.OrderEndBlockers
	initGen := app.ModuleManager.OrderInitGenesis
	require.NotEmpty(t, begin)
	require.NotEmpty(t, end)
	require.NotEmpty(t, initGen)

	split := indexOf(t, "BeginBlockers", begin, splitmoduletypes.ModuleName)
	for _, skimmer := range []string{shieldmoduletypes.ModuleName, repmoduletypes.ModuleName, federationmoduletypes.ModuleName} {
		require.Less(t, indexOf(t, "BeginBlockers", begin, skimmer), split,
			"%s skims the community pool and must run before split in BeginBlock", skimmer)
	}

	require.Less(t,
		indexOf(t, "EndBlockers", end, servicemoduletypes.ModuleName),
		indexOf(t, "EndBlockers", end, federationmoduletypes.ModuleName),
		"service's EndBlocker fires the operator hooks federation's pass depends on")
	require.Less(t,
		indexOf(t, "InitGenesis", initGen, servicemoduletypes.ModuleName),
		indexOf(t, "InitGenesis", initGen, federationmoduletypes.ModuleName),
		"federation's InitGenesis rebuilds bridge bindings against service operators")
}

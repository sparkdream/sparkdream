// anonymous_action_window_test.go checks, on every network's genesis, that
// the ZK key activation delay outlasts the federation arbiter vote window.
//
// An arbiter vote is an anonymous one-per-content action proven against the
// live member tree. A member who replaces their ZK key gets a fresh
// nullifier when the new key activates, zk_key_activation_delay epochs
// later; if an arbitration round were still open then, that member could
// cast two of the quorum's three votes. The build-tagged bounds in
// reptypes.MaxAnonymousActionWindow keep governance from opening that gap;
// this test checks the shipped values directly, independent of build tag
// (the earlier commons-side cap had no such check, and the bootstrap broke
// it on every network unnoticed, CMN-101).
package crossnetwork_test

import (
	"encoding/json"
	"testing"
	"time"

	"sparkdream/deploy/config/network/audit"
)

func TestZkKeyDelayOutlastsArbiterWindow(t *testing.T) {
	const blockSeconds = 6
	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			appState, ok, err := audit.LoadGenesis(baseDir, network)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Skipf("%s/genesis.json not yet present", network)
			}
			rep, err := audit.ModuleParams(appState, "rep")
			if err != nil {
				t.Fatal(err)
			}
			epochBlocks, err := audit.AsInt(rep["epoch_blocks"])
			if err != nil {
				t.Fatalf("rep.epoch_blocks: %v", err)
			}
			delayEpochs, err := audit.AsInt(rep["zk_key_activation_delay"])
			if err != nil {
				t.Fatalf("rep.zk_key_activation_delay: %v", err)
			}
			fed, err := audit.ModuleParams(appState, "federation")
			if err != nil {
				t.Fatal(err)
			}
			var windowStr string
			if err := json.Unmarshal(fed["arbiter_resolution_window"], &windowStr); err != nil {
				t.Fatalf("federation.arbiter_resolution_window: %v", err)
			}
			window, err := time.ParseDuration(windowStr)
			if err != nil {
				t.Fatalf("federation.arbiter_resolution_window %q: %v", windowStr, err)
			}

			delay := time.Duration(delayEpochs*epochBlocks*blockSeconds) * time.Second
			if delay <= window {
				t.Errorf("zk_key_activation_delay %d epochs * %d blocks = %s does not outlast arbiter_resolution_window %s",
					delayEpochs, epochBlocks, delay, window)
			}
		})
	}
}

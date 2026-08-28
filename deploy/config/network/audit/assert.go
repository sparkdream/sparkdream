package audit

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/cosmos/gogoproto/proto"
)

// AssertGenesisParams loads the genesis at genesisPath and asserts, for
// every project and SDK module the chain registers, that the params block:
//  1. Has every key the build-tag-active Params struct defines (missing).
//  2. Does not carry any key the struct doesn't recognize (extra/renamed).
//  3. Round-trips into the typed struct without error (catches type
//     mismatches like a quoted "5" where a uint64 is expected, and
//     validates nested structs such as trust_level_config).
//  4. Passes the module's own Params.Validate.
//  5. Carries the same VALUE the build-tag-active code default produces, for
//     every param the network's config.yml does not deliberately pin.
//
// configPath is the network's config.yml — the authoritative list of which
// params that network intends to diverge on. See AssertNoParamDrift.
//
// Each module gets its own subtest so a single network's drift surfaces
// every affected module at once instead of bailing on the first failure.
func AssertGenesisParams(t *testing.T, genesisPath, configPath string) {
	t.Helper()
	data, err := os.ReadFile(genesisPath)
	if err != nil {
		t.Fatalf("read %s: %v", genesisPath, err)
	}
	var doc struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", genesisPath, err)
	}

	pins, err := PinnedParamKeys(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}

	for _, group := range []struct {
		label   string
		modules map[string]func() proto.Message
	}{
		{"project", ProjectModules},
		{"sdk", SDKModules},
	} {
		for name, ctor := range group.modules {
			t.Run(group.label+"/"+name, func(t *testing.T) {
				assertModuleParams(t, doc.AppState, name, ZeroTemplate(ctor))
				assertNoModuleDrift(t, doc.AppState, name, ctor, pins[name])
			})
		}
	}
}

func assertModuleParams(t *testing.T, appState map[string]json.RawMessage, name string, params any) {
	t.Helper()
	modBytes, ok := appState[name]
	if !ok {
		t.Fatalf("module %q missing from genesis app_state", name)
	}
	var mod struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(modBytes, &mod); err != nil {
		t.Fatalf("parse %s genesis state: %v", name, err)
	}
	if len(mod.Params) == 0 {
		t.Fatalf("module %q has empty params block in genesis", name)
	}

	var actualKeys map[string]json.RawMessage
	if err := json.Unmarshal(mod.Params, &actualKeys); err != nil {
		t.Fatalf("parse %s params block: %v", name, err)
	}

	expected := ParamsKeys(params)

	var missing []string
	for k := range expected {
		if _, ok := actualKeys[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("genesis missing %d %s param(s): %v", len(missing), name, missing)
	}

	var extra []string
	for k := range actualKeys {
		if _, ok := expected[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("genesis has %d unrecognized %s param(s) (renamed/removed?): %v", len(extra), name, extra)
	}

	if err := RoundTripJSON(mod.Params, params); err != nil {
		t.Errorf("type-check round-trip failed for %s: %v", name, err)
		return // a value check on params that did not decode says nothing
	}

	if err := ValidateParams(mod.Params, params); err != nil {
		t.Errorf("%s params in genesis fail the module's own validation: %v", name, err)
	}
}

// assertNoModuleDrift fails when a param the network does not pin in its
// config.yml carries a different value in genesis than the build-tag-active
// code default produces.
//
// A failure here means the shipped genesis and the binary disagree. Either the
// genesis needs regenerating for a default that moved, or — if the divergence
// is intended — the value belongs in that network's config.yml, where it is
// visible as a deliberate choice instead of an accident of generation order.
func assertNoModuleDrift(t *testing.T, appState map[string]json.RawMessage, name string, ctor func() proto.Message, pinned map[string]struct{}) {
	t.Helper()
	modBytes, ok := appState[name]
	if !ok {
		return // absence is already reported by assertModuleParams
	}
	var mod struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(modBytes, &mod); err != nil || len(mod.Params) == 0 {
		return
	}
	drift, err := ParamDrift(mod.Params, ctor, pinned)
	if err != nil {
		t.Errorf("%s: compare params against code defaults: %v", name, err)
		return
	}
	for _, d := range drift {
		t.Errorf("%s params drifted from code defaults — %s\n"+
			"    regenerate this network's genesis, or pin the value in its config.yml if the divergence is intended", name, d)
	}
}

package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/cosmos/gogoproto/jsonpb"
	"github.com/cosmos/gogoproto/proto"
	"gopkg.in/yaml.v3"
)

// Value drift is the failure mode the key/type/Validate checks structurally
// cannot see: a param that is present, correctly typed, and individually valid,
// but carrying a number the binary no longer agrees with.
//
// It happens because a network's genesis.json is a GENERATED artifact — the
// values in it were the code defaults at the moment it was generated. Change a
// default in x/<mod>/types/params_vals_<network>.go and the shipped genesis
// keeps the old number, silently, forever. The chain then boots on parameters
// its own source disagrees with, and nothing in the tree says so.
//
// The check is only possible because config.yml is an explicit, machine-
// readable record of every param a network deliberately diverges on. Anything
// NOT listed there has no reason to differ from the code default, so:
//
//	expected(param) = config.yml pin if present, else DefaultParams() value
//
// Only the KEYS of the config.yml pins are used, never their values. A pinned
// param is skipped entirely rather than compared, which sidesteps the fact that
// YAML scalars and proto-JSON do not agree textually (`"0.15"` in config.yml
// becomes `"0.150000000000000000"` in genesis).

// PinnedParamKeys reads a network's config.yml and returns, per module, the set
// of param keys that network deliberately overrides.
//
// A pin on a NESTED field (e.g. trust_level_config.established_min_rep) marks
// the whole top-level param as pinned. That is intentional: the comparison
// operates on top-level params, so a partially-overridden nested object cannot
// be meaningfully diffed against the default.
func PinnedParamKeys(configPath string) (map[string]map[string]struct{}, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}
	out := map[string]map[string]struct{}{}
	genesis, _ := doc["genesis"].(map[string]any)
	appState, _ := genesis["app_state"].(map[string]any)
	for module, v := range appState {
		mod, ok := v.(map[string]any)
		if !ok {
			continue
		}
		params, ok := mod["params"].(map[string]any)
		if !ok {
			continue
		}
		keys := make(map[string]struct{}, len(params))
		for k := range params {
			keys[k] = struct{}{}
		}
		out[module] = keys
	}
	return out, nil
}

// ParamDrift compares a module's genesis params against its code defaults and
// returns one human-readable line per unpinned param whose value differs.
//
// Both sides are re-marshaled through the same proto-JSON marshaler before
// comparison, so encoding differences (Dec padding, uint64-as-string) cancel
// out and only real value differences survive.
func ParamDrift(genesisParams []byte, ctor func() proto.Message, pinned map[string]struct{}) ([]string, error) {
	want := ctor()
	got, err := decodeParams(genesisParams, ZeroTemplate(ctor))
	if err != nil {
		return nil, err
	}
	wantMap, err := protoJSONMap(want)
	if err != nil {
		return nil, err
	}
	gotMap, err := protoJSONMap(got)
	if err != nil {
		return nil, err
	}

	var drift []string
	for key, wantVal := range wantMap {
		if _, isPinned := pinned[key]; isPinned {
			continue
		}
		if gotVal, ok := gotMap[key]; !ok || !bytes.Equal(wantVal, gotVal) {
			drift = append(drift, fmt.Sprintf("%s: genesis has %s, code default is %s",
				key, orMissing(gotMap, key), wantVal))
		}
	}
	sort.Strings(drift)
	return drift, nil
}

func orMissing(m map[string]json.RawMessage, key string) string {
	if v, ok := m[key]; ok {
		return string(v)
	}
	return "(absent)"
}

// protoJSONMap marshals a params message to proto-JSON with original (snake_case)
// field names, matching both genesis.json and config.yml key spelling.
func protoJSONMap(m proto.Message) (map[string]json.RawMessage, error) {
	var buf bytes.Buffer
	if err := (&jsonpb.Marshaler{EmitDefaults: true, OrigName: true}).Marshal(&buf, m); err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

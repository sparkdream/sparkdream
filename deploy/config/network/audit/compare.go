package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"cosmossdk.io/math"
)

// LoadGenesis reads <baseDir>/<network>/genesis.json and returns the
// app_state map. The bool return is false if the file does not exist —
// useful while devnet/mainnet genesis files are still being prepared so
// callers can t.Skip rather than fail.
func LoadGenesis(baseDir, network string) (map[string]json.RawMessage, bool, error) {
	path := filepath.Join(baseDir, network, "genesis.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var doc struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc.AppState, true, nil
}

// ModuleParams extracts the params block for a module from an app_state map.
func ModuleParams(appState map[string]json.RawMessage, module string) (map[string]json.RawMessage, error) {
	raw, ok := appState[module]
	if !ok {
		return nil, fmt.Errorf("module %q missing from app_state", module)
	}
	var mod struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &mod); err != nil {
		return nil, fmt.Errorf("parse %s params: %w", module, err)
	}
	if mod.Params == nil {
		return nil, fmt.Errorf("module %q has no params block", module)
	}
	return mod.Params, nil
}

// NestedObject drills into a JSON object key inside a params map. Used for
// fields like trust_level_config whose value is itself a struct.
func NestedObject(params map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("nested params %q missing", key)
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, fmt.Errorf("parse nested %q: %w", key, err)
	}
	return nested, nil
}

// AsInt parses a JSON value as int64. Handles both bare numbers (proto
// int32, bool) and quoted strings (proto int64/uint64, which the proto3
// JSON spec requires to be quoted to avoid JS precision loss).
func AsInt(raw json.RawMessage) (int64, error) {
	s := string(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return strconv.ParseInt(s, 10, 64)
}

// AsDec parses a JSON value as a Cosmos LegacyDec. Used for fields like
// provisional_min_rep that ship as decimal strings (e.g. "50.000000000000000000").
func AsDec(raw json.RawMessage) (math.LegacyDec, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return math.LegacyDec{}, fmt.Errorf("expected string, got %s", raw)
	}
	return math.LegacyNewDecFromStr(s)
}

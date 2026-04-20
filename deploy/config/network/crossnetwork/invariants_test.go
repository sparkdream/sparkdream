// Package crossnetwork_test asserts numeric invariants between the devnet,
// testnet, and mainnet genesis files. Lives in its own package (no build
// tag) so it loads all three genesis files at once and verifies their
// relative ordering.
//
// Default ordering rules:
//   - Difficulty thresholds (min reputation, min interims, min seasons,
//     bond requirements) ascend devnet → testnet → mainnet.
//   - Generosity (invitation credits, larger queues, more lenient windows)
//     descends devnet → testnet → mainnet.
//
// Intentional exceptions go in allowedInversions with a documented reason,
// so future-you can decide whether the exception still applies.
package crossnetwork_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"cosmossdk.io/math"

	"sparkdream/deploy/config/network/audit"
)

const baseDir = ".."

// allowedInversions documents intentional violations of the standard
// devnet → testnet → mainnet ordering. Keyed by "<module>.<dotted_path>".
// Always include rationale so future-you can decide whether the exception
// still applies after a parameter rebalance.
var allowedInversions = map[string]string{
	// testnet (2) > mainnet (1): testnet deliberately stricter so the
	// season-transition flow gets exercised twice as often during validation.
	// Production sits at 1; the inversion is a testing convenience, not a
	// design drift.
	"rep.trust_level_config.trusted_min_seasons": "testnet sets TrustedMinSeasons=2 to exercise season transitions more aggressively; mainnet stays at the production value of 1",
}

type netVals struct{ devnet, testnet, mainnet map[string]json.RawMessage }

func loadModule(t *testing.T, module string) netVals {
	t.Helper()
	var v netVals
	for name, dst := range map[string]*map[string]json.RawMessage{
		"devnet":  &v.devnet,
		"testnet": &v.testnet,
		"mainnet": &v.mainnet,
	} {
		appState, ok, err := audit.LoadGenesis(baseDir, name)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Skipf("%s/genesis.json not yet present — skipping cross-network invariants", name)
		}
		params, err := audit.ModuleParams(appState, module)
		if err != nil {
			t.Fatal(err)
		}
		*dst = params
	}
	return v
}

func nested(t *testing.T, v netVals, key string) netVals {
	t.Helper()
	do := func(p map[string]json.RawMessage) map[string]json.RawMessage {
		n, err := audit.NestedObject(p, key)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	return netVals{do(v.devnet), do(v.testnet), do(v.mainnet)}
}

func intsAsc(t *testing.T, v netVals, ctxPath, field string) {
	t.Helper()
	d, tn, mn := mustInt(t, v.devnet[field], field, "devnet"),
		mustInt(t, v.testnet[field], field, "testnet"),
		mustInt(t, v.mainnet[field], field, "mainnet")
	key := ctxPath + "." + field
	if d > tn && allowedInversions[key] == "" {
		t.Errorf("%s: devnet (%d) > testnet (%d); expected ≤ (add to allowedInversions if intentional)", key, d, tn)
	}
	if tn > mn && allowedInversions[key] == "" {
		t.Errorf("%s: testnet (%d) > mainnet (%d); expected ≤ (add to allowedInversions if intentional)", key, tn, mn)
	}
}

func intsDesc(t *testing.T, v netVals, ctxPath, field string) {
	t.Helper()
	d, tn, mn := mustInt(t, v.devnet[field], field, "devnet"),
		mustInt(t, v.testnet[field], field, "testnet"),
		mustInt(t, v.mainnet[field], field, "mainnet")
	key := ctxPath + "." + field
	if d < tn && allowedInversions[key] == "" {
		t.Errorf("%s: devnet (%d) < testnet (%d); expected ≥ (add to allowedInversions if intentional)", key, d, tn)
	}
	if tn < mn && allowedInversions[key] == "" {
		t.Errorf("%s: testnet (%d) < mainnet (%d); expected ≥ (add to allowedInversions if intentional)", key, tn, mn)
	}
}

func decsAsc(t *testing.T, v netVals, ctxPath, field string) {
	t.Helper()
	d := mustDec(t, v.devnet[field], field, "devnet")
	tn := mustDec(t, v.testnet[field], field, "testnet")
	mn := mustDec(t, v.mainnet[field], field, "mainnet")
	key := ctxPath + "." + field
	if d.GT(tn) && allowedInversions[key] == "" {
		t.Errorf("%s: devnet (%s) > testnet (%s); expected ≤ (add to allowedInversions if intentional)", key, d, tn)
	}
	if tn.GT(mn) && allowedInversions[key] == "" {
		t.Errorf("%s: testnet (%s) > mainnet (%s); expected ≤ (add to allowedInversions if intentional)", key, tn, mn)
	}
}

func mustInt(t *testing.T, raw json.RawMessage, field, network string) int64 {
	t.Helper()
	if raw == nil {
		t.Fatalf("%s missing on %s", field, network)
	}
	n, err := audit.AsInt(raw)
	if err != nil {
		t.Fatalf("parse %s on %s as int: %v (raw=%s)", field, network, err, raw)
	}
	return n
}

func mustDec(t *testing.T, raw json.RawMessage, field, network string) math.LegacyDec {
	t.Helper()
	if raw == nil {
		t.Fatalf("%s missing on %s", field, network)
	}
	d, err := audit.AsDec(raw)
	if err != nil {
		t.Fatalf("parse %s on %s as dec: %v (raw=%s)", field, network, err, raw)
	}
	return d
}

func TestRepTrustThresholds(t *testing.T) {
	rep := loadModule(t, "rep")
	tlc := nested(t, rep, "trust_level_config")
	ctx := "rep.trust_level_config"

	for _, f := range []string{"provisional_min_rep", "established_min_rep", "trusted_min_rep", "core_min_rep"} {
		t.Run(f, func(t *testing.T) { decsAsc(t, tlc, ctx, f) })
	}
	for _, f := range []string{"provisional_min_interims", "established_min_interims", "trusted_min_seasons", "core_min_seasons"} {
		t.Run(f, func(t *testing.T) { intsAsc(t, tlc, ctx, f) })
	}
	for _, f := range []string{"provisional_invitation_credits", "established_invitation_credits", "trusted_invitation_credits", "core_invitation_credits"} {
		t.Run(f, func(t *testing.T) { intsDesc(t, tlc, ctx, f) })
	}
}

func TestRepSentinelEpoch(t *testing.T) {
	rep := loadModule(t, "rep")
	intsAsc(t, rep, "rep", "sentinel_reward_epoch_blocks")
}

func TestShieldHardening(t *testing.T) {
	sh := loadModule(t, "shield")
	intsAsc(t, sh, "shield", "min_tle_validators")
	intsAsc(t, sh, "shield", "min_batch_size")
}

// allowedVariations documents per-network parameter values that are
// intentionally different. Anything not listed must be byte-equal across
// devnet, testnet, and mainnet — that's the default contract for params
// (immutable design, universal protocol constants).
//
// Keyed by full dotted path under app_state, e.g. "rep.params.epoch_blocks".
// The value is a short rationale; if you can't write one, the value
// probably should not differ.
var allowedVariations = map[string]string{
	// === gov: governance cadence scales per network ===
	"gov.params.max_deposit_period":      "deliberation window varies: shorter on devnet for iteration, longer on mainnet",
	"gov.params.voting_period":           "voting window varies: shorter on devnet for iteration, longer on mainnet",
	"gov.params.expedited_voting_period": "expedited window varies per network",

	// === season: epoch + season cadence + retro PGF tuning ===
	"season.params.epoch_blocks":                      "epoch length varies per network",
	"season.params.season_duration_epochs":            "season length varies per network",
	"season.params.transition_grace_period":           "transition grace varies per network",
	"season.params.transition_batch_size":             "devnet uses smaller batch (50) for faster transition rounds",
	"season.params.display_name_appeal_period_blocks": "appeal window varies per network",
	"season.params.nomination_min_stake":              "lower min stake on devnet to make retroactive PGF reachable in tests",
	"season.params.retro_reward_budget_min":           "smaller retro PGF floor on devnet",
	"season.params.retro_reward_budget_max":           "smaller retro PGF ceiling on devnet",
	"season.params.retro_reward_min_conviction":       "lower conviction threshold on devnet",
	"season.season.end_block":                         "derived from epoch_blocks * season_duration_epochs (varies per network)",
	"season.season.original_end_block":                "derived from epoch_blocks * season_duration_epochs (varies per network)",

	// === rep: timing, jury sizing, build-tag conditional values ===
	"rep.params.epoch_blocks":                       "matches season.epoch_blocks per network",
	"rep.params.season_duration_epochs":             "matches season.season_duration_epochs per network",
	"rep.params.conviction_half_life_epochs":        "conviction decay scales per network",
	"rep.params.default_review_period_epochs":       "review window varies per network",
	"rep.params.default_challenge_period_epochs":    "challenge window varies per network",
	"rep.params.invitation_accountability_epochs":   "invitation lock varies per network (1 season each)",
	"rep.params.interim_deadline_epochs":            "interim deadline varies per network",
	"rep.params.challenge_response_deadline_epochs": "challenge response window varies per network",
	"rep.params.gift_cooldown_blocks":               "gift cooldown matches epoch length per network",
	"rep.params.jury_size":                          "devnet uses smaller jury (3) due to small test member set",
	"rep.params.min_juror_reputation":               "devnet lowers juror rep threshold for testing",
	"rep.params.sentinel_reward_epoch_blocks":       "build-tag conditional cadence (devnet 6h / testnet 12h / mainnet 24h)",
	"rep.params.trust_level_config.provisional_min_rep":            "build-tag conditional",
	"rep.params.trust_level_config.provisional_min_interims":       "build-tag conditional",
	"rep.params.trust_level_config.established_min_rep":            "build-tag conditional",
	"rep.params.trust_level_config.established_min_interims":       "build-tag conditional",
	"rep.params.trust_level_config.trusted_min_rep":                "build-tag conditional",
	"rep.params.trust_level_config.trusted_min_seasons":            "build-tag conditional (testnet=2 deliberately stricter than mainnet=1)",
	"rep.params.trust_level_config.core_min_rep":                   "build-tag conditional",
	"rep.params.trust_level_config.core_min_seasons":               "build-tag conditional",
	"rep.params.trust_level_config.provisional_invitation_credits": "build-tag conditional",
	"rep.params.trust_level_config.established_invitation_credits": "build-tag conditional",
	"rep.params.trust_level_config.trusted_invitation_credits":     "build-tag conditional",
	"rep.params.trust_level_config.core_invitation_credits":        "build-tag conditional",

	// === forum: archive + appeal cadence ===
	"forum.params.archive_threshold":    "archive window varies per network",
	"forum.params.archive_cooldown":     "re-archive cooldown varies per network",
	"forum.params.ephemeral_ttl":        "ephemeral retention varies per network",
	"forum.params.hide_appeal_cooldown": "appeal cooldown varies per network",
	"forum.params.lock_appeal_cooldown": "appeal cooldown varies per network",
	"forum.params.move_appeal_cooldown": "appeal cooldown varies per network",

	// === federation: timing + fee + capacity, all build-tag conditional
	// (see x/federation/types/genesis_vals_*.go) ===
	"federation.params.arbiter_escalation_window":      "build-tag conditional",
	"federation.params.arbiter_resolution_window":      "build-tag conditional",
	"federation.params.attestation_ttl":                "build-tag conditional",
	"federation.params.bridge_revocation_cooldown":     "build-tag conditional",
	"federation.params.bridge_unbonding_period":        "build-tag conditional",
	"federation.params.challenge_cooldown":             "build-tag conditional",
	"federation.params.challenge_fee.amount":           "build-tag conditional fee scales per network",
	"federation.params.challenge_jury_deadline":        "build-tag conditional",
	"federation.params.challenge_ttl":                  "build-tag conditional",
	"federation.params.challenge_window":               "build-tag conditional",
	"federation.params.content_ttl":                    "build-tag conditional",
	"federation.params.escalation_fee.amount":          "build-tag conditional fee scales per network",
	"federation.params.ibc_packet_timeout":             "build-tag conditional",
	"federation.params.min_bridge_stake.amount":        "build-tag conditional bridge stake (production stricter)",
	"federation.params.rate_limit_window":              "build-tag conditional",
	"federation.params.unverified_link_ttl":            "build-tag conditional",
	"federation.params.verification_window":            "build-tag conditional",
	"federation.params.verifier_demotion_cooldown":     "build-tag conditional",
	"federation.params.verifier_overturn_base_cooldown": "build-tag conditional",

	// === shield: TLE/DKG cadence + capacity hardening ===
	"shield.params.dkg_window_blocks":      "DKG window varies per network",
	"shield.params.shield_epoch_interval":  "epoch interval varies per network",
	"shield.params.tle_jail_duration":      "jail duration varies per network",
	"shield.params.tle_miss_tolerance":     "miss tolerance varies per network",
	"shield.params.tle_miss_window":        "miss window varies per network",
	"shield.params.min_tle_validators":     "production stricter (devnet=3 / testnet=5)",
	"shield.params.min_batch_size":         "production stricter (devnet=1 / testnet=3)",
	"shield.params.max_pending_queue_size": "production has larger queue",
	"shield.params.min_gas_reserve":        "production has larger gas reserve",
}

// uniformAppStatePaths lists JSON paths under app_state that should be
// byte-equal across all three networks, in addition to every module's
// `params` block. These are universal protocol constants (token metadata,
// governance categories, etc.) — genuinely network-specific data like
// accounts, balances, member maps, gen_txs, council members, and member
// profiles is intentionally excluded.
var uniformAppStatePaths = []string{
	"bank.denom_metadata",
	"commons.category_map",
	"commons.next_category_id",
	"crisis.constant_fee",
}

// TestParamsEqualAcrossNetworks recursively walks every module's `params`
// block AND each path in uniformAppStatePaths, asserting byte-equality
// across devnet, testnet, and mainnet. Any leaf path that legitimately
// differs must appear in allowedVariations with rationale; otherwise the
// test fails with the exact differing values.
//
// Catches the category of bug where a per-network override silently drifts
// from the immutable-design baseline (e.g. mint inflation falling back to
// SDK defaults instead of the configured 2-5% range, or denom_metadata
// shape diverging due to JSON-marshaler-version differences).
func TestParamsEqualAcrossNetworks(t *testing.T) {
	devnetState := mustAppState(t, "devnet")
	testnetState := mustAppState(t, "testnet")
	mainnetState := mustAppState(t, "mainnet")

	modules := allModuleNames()
	for _, module := range modules {
		t.Run(module, func(t *testing.T) {
			d, dOk := moduleParamsRaw(devnetState, module)
			tn, tOk := moduleParamsRaw(testnetState, module)
			m, mOk := moduleParamsRaw(mainnetState, module)
			if !dOk || !tOk || !mOk {
				t.Skipf("module %q has no params on at least one network", module)
				return
			}
			compareValue(t, module+".params", d, tn, m)
		})
	}

	for _, path := range uniformAppStatePaths {
		t.Run(path, func(t *testing.T) {
			d := lookupPath(devnetState, path)
			tn := lookupPath(testnetState, path)
			m := lookupPath(mainnetState, path)
			compareValue(t, path, d, tn, m)
		})
	}
}

// lookupPath walks a dotted path under app_state, returning the raw JSON
// at that location or nil if any segment is missing.
func lookupPath(appState map[string]json.RawMessage, dottedPath string) json.RawMessage {
	parts := strings.Split(dottedPath, ".")
	raw, ok := appState[parts[0]]
	if !ok {
		return nil
	}
	for _, p := range parts[1:] {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		raw = obj[p]
		if raw == nil {
			return nil
		}
	}
	return raw
}

func mustAppState(t *testing.T, network string) map[string]json.RawMessage {
	t.Helper()
	as, ok, err := audit.LoadGenesis(baseDir, network)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skipf("%s/genesis.json not yet present", network)
	}
	return as
}

func moduleParamsRaw(appState map[string]json.RawMessage, module string) (json.RawMessage, bool) {
	raw, ok := appState[module]
	if !ok {
		return nil, false
	}
	var mod struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &mod); err != nil {
		return nil, false
	}
	if len(mod.Params) == 0 {
		return nil, false
	}
	return mod.Params, true
}

func allModuleNames() []string {
	out := make([]string, 0, len(audit.ProjectModules)+len(audit.SDKModules))
	for k := range audit.ProjectModules {
		out = append(out, k)
	}
	for k := range audit.SDKModules {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// compareValue recursively compares the same JSON path across three
// networks. If they're byte-equal, returns. If they're all objects,
// recurses into each key. Otherwise reports a leaf-level mismatch unless
// the path is in allowedVariations.
func compareValue(t *testing.T, path string, d, tn, m json.RawMessage) {
	t.Helper()
	if bytes.Equal(d, tn) && bytes.Equal(tn, m) {
		return
	}

	if d == nil || tn == nil || m == nil {
		if _, ok := allowedVariations[path]; ok {
			return
		}
		t.Errorf("%s: present on some networks, missing on others (devnet=%v testnet=%v mainnet=%v)",
			path, d != nil, tn != nil, m != nil)
		return
	}

	var dObj, tnObj, mObj map[string]json.RawMessage
	if json.Unmarshal(d, &dObj) == nil &&
		json.Unmarshal(tn, &tnObj) == nil &&
		json.Unmarshal(m, &mObj) == nil &&
		dObj != nil && tnObj != nil && mObj != nil {
		keys := make(map[string]struct{})
		for k := range dObj {
			keys[k] = struct{}{}
		}
		for k := range tnObj {
			keys[k] = struct{}{}
		}
		for k := range mObj {
			keys[k] = struct{}{}
		}
		sortedKeys := make([]string, 0, len(keys))
		for k := range keys {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)
		for _, k := range sortedKeys {
			compareValue(t, path+"."+k, dObj[k], tnObj[k], mObj[k])
		}
		return
	}

	if _, ok := allowedVariations[path]; ok {
		return
	}
	t.Errorf("%s: values differ; expected identical across networks (devnet=%s testnet=%s mainnet=%s) — add to allowedVariations if intentional",
		path, d, tn, m)
}

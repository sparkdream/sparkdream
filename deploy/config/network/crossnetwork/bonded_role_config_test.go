// Seeded-state drift guard. The per-network genesis_audit_test.go files and
// TestParamsEqualAcrossNetworks both walk `app_state.<module>.params` — so a
// module's *other* generated genesis state has no coverage at all.
//
// x/rep's bonded_role_config_list is the case that matters. Every one of its
// four roles is overwritten during InitGenesis by a write-through from the
// owning module's params (x/forum, x/collect and x/federation for the first
// three; x/rep itself for ROLE_TYPE_INITIATIVE_REVIEWER, via
// SyncReviewerBondedRoleConfig), so the seeded list is what a chain runs on
// only if a write-through is ever skipped. Keeping it in step with the Go
// defaults is cheap and leaves nothing to reason about. Live (2026-08-27):
// all three networks shipped `min_rep_tier: 3` for the reviewer after the Go
// default moved to 0, back when no module wrote that role through, which
// would have locked the role out of a fresh chain for good — the exact shape
// of the jury_size miss two days earlier.
package crossnetwork_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cosmos/gogoproto/jsonpb"

	"sparkdream/deploy/config/network/audit"
	reptypes "sparkdream/x/rep/types"
)

func TestRepBondedRoleConfigsMatchDefaults(t *testing.T) {
	want := reptypes.DefaultBondedRoleConfigs()

	for _, network := range []string{"devnet", "testnet", "mainnet"} {
		t.Run(network, func(t *testing.T) {
			appState, ok, err := audit.LoadGenesis(baseDir, network)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Skipf("%s/genesis.json not yet present", network)
			}

			raw, present, err := audit.ModuleField(appState, "rep", "bonded_role_config_list")
			if err != nil {
				t.Fatal(err)
			}
			if !present {
				t.Fatalf("rep.bonded_role_config_list missing from %s genesis; "+
					"regenerate with deploy/scripts/regenerate-network-genesis.py", network)
			}

			// Decode through jsonpb, not encoding/json: genesis writes uint64
			// and int64 as JSON strings and enums as their names, neither of
			// which the stdlib decoder accepts into the generated struct.
			var elems []json.RawMessage
			if err := json.Unmarshal(raw, &elems); err != nil {
				t.Fatalf("parse rep.bonded_role_config_list on %s: %v", network, err)
			}

			var got []reptypes.BondedRoleConfig
			for _, elem := range elems {
				var cfg reptypes.BondedRoleConfig
				u := &jsonpb.Unmarshaler{AllowUnknownFields: false}
				if err := u.Unmarshal(bytes.NewReader(elem), &cfg); err != nil {
					t.Fatalf("decode bonded role config on %s: %v (raw=%s)", network, err, elem)
				}
				got = append(got, cfg)
			}

			if len(got) != len(want) {
				t.Fatalf("%s has %d bonded role configs, DefaultBondedRoleConfigs has %d; "+
					"regenerate with deploy/scripts/regenerate-network-genesis.py",
					network, len(got), len(want))
			}

			// Compared position-wise: the list is generated straight from
			// DefaultBondedRoleConfigs, so a reordering is itself drift.
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s bonded_role_config_list[%d] (%s) drifted from the Go default;\n  genesis: %+v\n  default: %+v\n"+
						"regenerate with deploy/scripts/regenerate-network-genesis.py",
						network, i, want[i].RoleType, got[i], want[i])
				}
			}
		})
	}
}

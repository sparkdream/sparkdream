// Seeded-state drift guard for x/shield, which the params walkers don't see:
// registered_ops and verification_keys sit beside params, not inside them.
//
// registered_ops is generated from DefaultGenesis, so a hand edit or a stale
// regenerate is drift (e.g. a content op left at min_trust_level 1 after the
// default moved to 0 would quietly shrink the anonymity set on one network).
//
// verification_keys is pasted in from a ceremony run. Nothing parses it at
// InitGenesis: a truncated or mis-encoded key only shows up when every
// MsgShieldedExec fails proof verification, so it is parsed here instead.
package crossnetwork_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/cosmos/gogoproto/jsonpb"
	gogoproto "github.com/cosmos/gogoproto/proto"

	"sparkdream/deploy/config/network/audit"
	shieldtypes "sparkdream/x/shield/types"
)

// shieldField decodes app_state.shield.<field> into a list of raw elements,
// skipping the subtest if the network's genesis isn't generated yet.
func shieldField(t *testing.T, network, field string) []json.RawMessage {
	t.Helper()
	appState, ok, err := audit.LoadGenesis(baseDir, network)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skipf("%s/genesis.json not yet present", network)
	}
	raw, present, err := audit.ModuleField(appState, "shield", field)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatalf("shield.%s missing from %s genesis; "+
			"regenerate with deploy/scripts/regenerate-network-genesis.py", field, network)
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		t.Fatalf("parse shield.%s on %s: %v", field, network, err)
	}
	return elems
}

func TestShieldRegisteredOpsMatchDefaults(t *testing.T) {
	want := make(map[string]shieldtypes.ShieldedOpRegistration)
	for _, op := range shieldtypes.DefaultGenesis().RegisteredOps {
		want[op.MessageTypeUrl] = op
	}

	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			elems := shieldField(t, network, "registered_ops")
			if len(elems) != len(want) {
				t.Fatalf("%s has %d registered shield ops, DefaultGenesis has %d; "+
					"regenerate with deploy/scripts/regenerate-network-genesis.py",
					network, len(elems), len(want))
			}
			for _, elem := range elems {
				// jsonpb, not encoding/json: genesis writes uint64 as strings
				// and enums as names.
				var got shieldtypes.ShieldedOpRegistration
				u := &jsonpb.Unmarshaler{AllowUnknownFields: false}
				if err := u.Unmarshal(bytes.NewReader(elem), &got); err != nil {
					t.Fatalf("decode shield op on %s: %v (raw=%s)", network, err, elem)
				}
				def, ok := want[got.MessageTypeUrl]
				if !ok {
					t.Errorf("%s registers %s, which DefaultGenesis does not", network, got.MessageTypeUrl)
					continue
				}
				if !gogoproto.Equal(&got, &def) {
					t.Errorf("%s shield op %s drifted from the Go default;\n  genesis: %+v\n  default: %+v\n"+
						"regenerate with deploy/scripts/regenerate-network-genesis.py",
						network, got.MessageTypeUrl, got, def)
				}
			}
		})
	}
}

func TestShieldVerificationKeysParse(t *testing.T) {
	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			// Mainnet ships none until its ceremony; an empty list is valid
			// (production builds then refuse every exec).
			for _, elem := range shieldField(t, network, "verification_keys") {
				var vk shieldtypes.VerificationKey
				u := &jsonpb.Unmarshaler{AllowUnknownFields: false}
				if err := u.Unmarshal(bytes.NewReader(elem), &vk); err != nil {
					t.Fatalf("decode verification key on %s: %v", network, err)
				}
				if vk.CircuitId != shieldtypes.ShieldCircuitID {
					t.Errorf("%s stores a key for circuit %q; the keeper only looks up %q",
						network, vk.CircuitId, shieldtypes.ShieldCircuitID)
				}
				parsed := groth16.NewVerifyingKey(ecc.BN254)
				n, err := parsed.ReadFrom(bytes.NewReader(vk.VkBytes))
				if err != nil {
					t.Fatalf("%s %s verifying key does not parse as BN254 Groth16: %v",
						network, vk.CircuitId, err)
				}
				if int(n) != len(vk.VkBytes) {
					t.Errorf("%s %s verifying key has %d trailing bytes after the key",
						network, vk.CircuitId, len(vk.VkBytes)-int(n))
				}
			}
		})
	}
}

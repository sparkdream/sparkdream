// Identity guard for the committed network geneses (IDN-06 / IDN-12).
//
// x/identity's InitGenesis writes the canonical native bank metadata and
// seals it; bank's InitGenesis runs afterwards through its raw keeper, so a
// native denom_metadata entry left in the genesis overwrote the seal at
// block 0 on every network while the invariant suite (which only imports
// e2e snapshots) passed. This runs each committed genesis through the same
// pre-init rewrite the chain runs at InitChain and checks that it is
// accepted and leaves no native entry for bank to write.
package crossnetwork_test

import (
	"encoding/json"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"sparkdream/deploy/config/network/audit"
	"sparkdream/x/identity/genesisinit"
)

func TestNetworkGenesisNativeMetadataIsIdentitys(t *testing.T) {
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			appState, ok, err := audit.LoadGenesis(baseDir, network)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Skipf("%s/genesis.json not yet present", network)
			}
			rawAppState, err := json.Marshal(appState)
			if err != nil {
				t.Fatal(err)
			}
			out, id, err := genesisinit.RewriteSentinels(cdc, rawAppState)
			if err != nil {
				t.Fatalf("%s genesis is refused by the pre-init rewrite: %v", network, err)
			}
			var as struct {
				Bank struct {
					DenomMetadata []banktypes.Metadata `json:"denom_metadata"`
				} `json:"bank"`
			}
			if err := json.Unmarshal(out, &as); err != nil {
				t.Fatalf("parse rewritten %s app_state: %v", network, err)
			}
			for _, m := range as.Bank.DenomMetadata {
				if m.Base == id.BondDenom || m.Base == id.DreamDenom {
					t.Errorf("%s: bank.denom_metadata still carries native %q after the rewrite; bank would overwrite identity's seal", network, m.Base)
				}
			}
		})
	}
}

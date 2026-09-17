// chain_env_test.go asserts that each network's chain.env agrees with that
// network's genesis.json.
//
// chain.env is the launcher's source of truth: `envsubst` renders it into
// app.toml / client.toml / config.toml before a node starts. Nothing else
// validates it, which is how two bugs shipped at once:
//
//   - MIN_GAS_PRICES was "25000<denom>" on all three networks. That field is a
//     price PER GAS UNIT, not a flat fee; the value was a `--fees` amount
//     copied into the wrong field. At 200k gas it charged 5,000 SPARK per
//     transaction, which the testnet sentry enforced and which made that chain
//     unusable for anything.
//   - devnet's DENOM said uspark.sparkdreamdev while the running chain used
//     usparz.sparkdreamdev (the denom changed on a relaunch). Rendering that
//     into app.toml sets a minimum in a denom the chain does not have, and the
//     ante handler then rejects every transaction.
//
// Both are silent until a node is already deployed, so they belong in CI.
package crossnetwork_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// maxSaneGasPrice bounds MIN_GAS_PRICES from above. The value is a price per
// gas unit, so anything at or above 1 means a minimal 200k-gas transaction
// costs 200,000+ micro-units before it does anything useful. The real values
// sit around 0.025, so this leaves ~40x headroom while still catching a flat
// fee pasted into the field by four orders of magnitude.
const maxSaneGasPrice = "1.0"

var networks = []string{"devnet", "testnet", "mainnet"}

// readChainEnv parses the KEY="value" assignments out of a chain.env. It is
// deliberately not a shell: the file is a flat list of literals, and anything
// fancier than that would be a reason to fail loudly rather than interpret.
func readChainEnv(t *testing.T, network string) map[string]string {
	t.Helper()
	path := filepath.Join(baseDir, network, "chain.env")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	vals := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return vals
}

// genesisDoc returns the fields of a network's genesis.json that chain.env has
// to agree with. audit.LoadGenesis only surfaces app_state, and chain_id lives
// above it.
func genesisDoc(t *testing.T, network string) (chainID, bondDenom string, ok bool) {
	t.Helper()
	path := filepath.Join(baseDir, network, "genesis.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		ChainID  string `json:"chain_id"`
		AppState struct {
			Staking struct {
				Params struct {
					BondDenom string `json:"bond_denom"`
				} `json:"params"`
			} `json:"staking"`
		} `json:"app_state"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc.ChainID, doc.AppState.Staking.Params.BondDenom, true
}

// TestChainEnvMinGasPrices checks that MIN_GAS_PRICES is a well-formed,
// plausible price per gas unit denominated in the chain's own bond denom.
func TestChainEnvMinGasPrices(t *testing.T) {
	ceiling := math.LegacyMustNewDecFromStr(maxSaneGasPrice)

	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			env := readChainEnv(t, network)

			raw, ok := env["MIN_GAS_PRICES"]
			if !ok {
				t.Fatal("MIN_GAS_PRICES not set")
			}

			// Parse it exactly as the node does when it reads app.toml.
			coins, err := sdk.ParseDecCoins(raw)
			if err != nil {
				t.Fatalf("MIN_GAS_PRICES=%q does not parse as DecCoins: %v", raw, err)
			}
			if len(coins) != 1 {
				t.Fatalf("MIN_GAS_PRICES=%q has %d coins; expected exactly one", raw, len(coins))
			}
			price := coins[0]

			if !price.Amount.IsPositive() {
				t.Errorf("MIN_GAS_PRICES=%q is not positive; a node with no floor accepts zero-fee spam", raw)
			}
			if price.Amount.GTE(ceiling) {
				t.Errorf("MIN_GAS_PRICES=%q: %s per gas unit is implausibly high (ceiling %s). "+
					"This field is a price PER GAS UNIT, not a flat fee -- at 200k gas that is %s micro-units a transaction",
					raw, price.Amount, ceiling, price.Amount.MulInt64(200_000))
			}

			denom, ok := env["DENOM"]
			if !ok {
				t.Fatal("DENOM not set")
			}
			if price.Denom != denom {
				t.Errorf("MIN_GAS_PRICES denom %q != DENOM %q; the node would demand a fee in a denom the chain does not use, rejecting every transaction",
					price.Denom, denom)
			}
		})
	}
}

// TestChainEnvMatchesGenesis checks chain.env against the committed genesis for
// the same network, so a relaunch that changes the chain id or bond denom
// cannot leave the launcher pointed at the old one.
func TestChainEnvMatchesGenesis(t *testing.T) {
	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			env := readChainEnv(t, network)
			chainID, bondDenom, ok := genesisDoc(t, network)
			if !ok {
				t.Skipf("%s/genesis.json not present", network)
			}

			if got := env["CHAIN_ID"]; got != chainID {
				t.Errorf("CHAIN_ID=%q but genesis.json chain_id=%q", got, chainID)
			}
			if got := env["DENOM"]; got != bondDenom {
				t.Errorf("DENOM=%q but genesis.json staking bond_denom=%q", got, bondDenom)
			}
		})
	}
}

// TestChainEnvDenomsDistinct guards the property that makes the denoms worth
// checking at all: each network has its own, so a config copied between
// networks fails loudly instead of quietly pointing a testnet node at mainnet
// values.
//
// The gate here is DENOM itself, not the presence of a genesis.json: a network
// with a chain.env and no committed genesis is exactly the copy-paste case this
// test exists to catch, and skipping it would drop that network out of the
// comparison silently.
func TestChainEnvDenomsDistinct(t *testing.T) {
	seen := make(map[string]string)
	for _, network := range networks {
		denom, ok := readChainEnv(t, network)["DENOM"]
		if !ok {
			t.Errorf("%s: DENOM not set", network)
			continue
		}
		if prev, dup := seen[denom]; dup {
			t.Errorf("DENOM %q used by both %s and %s", denom, prev, network)
		}
		seen[denom] = network
	}
}

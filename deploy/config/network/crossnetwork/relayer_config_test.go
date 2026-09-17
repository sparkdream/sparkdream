// relayer_config_test.go asserts that deploy/relayer/hermes_config.toml agrees
// with the network configs it relays for.
//
// The hermes config restates facts that are already committed under
// config/network: the chain id, the bech32 prefix, and the fee denom, once per
// [[chains]] block. Nothing reconciled the two, and the drift is silent in the
// worst way -- a relayer with the wrong denom authenticates, connects, and then
// fails every transaction it submits with an insufficient-fee error that reads
// like a funding problem. That is exactly what happened when devnet's bond
// denom changed to usparz.sparkdreamdev on a relaunch: the committed chain.env
// said one thing, the running chain another, and the hermes config was
// hand-corrected against the node with a comment explaining the disagreement.
//
// Networks absent from the hermes config are not an error -- mainnet has no
// relayer yet. A hermes chain id that matches no network IS an error: it means
// the relayer is pointed somewhere this repo does not describe.
package crossnetwork_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const hermesConfigPath = "../../../relayer/hermes_config.toml"

// bech32Prefix is the chain's account prefix. It is a compile-time constant of
// the binary (app.Bech32Prefix), not a per-network setting, so every network's
// relayer block must carry this exact value.
const bech32Prefix = "sprkdrm"

type hermesChain struct {
	line     int
	id       string
	prefix   string
	gasPrice string
	gasDenom string
}

var (
	reChainID  = regexp.MustCompile(`^id\s*=\s*['"]([^'"]+)['"]`)
	rePrefix   = regexp.MustCompile(`^account_prefix\s*=\s*['"]([^'"]+)['"]`)
	reGasPrice = regexp.MustCompile(`^gas_price\s*=\s*\{\s*price\s*=\s*([0-9.]+)\s*,\s*denom\s*=\s*['"]([^'"]+)['"]\s*\}`)
)

// parseHermesChains pulls the [[chains]] blocks out of the hermes config.
// Deliberately line-based rather than a TOML decode: the file is hand-edited
// and heavily commented, and a line number in a failure message is worth more
// than the generality a full decode would buy.
func parseHermesChains(t *testing.T) []hermesChain {
	t.Helper()
	data, err := os.ReadFile(hermesConfigPath)
	if err != nil {
		t.Fatalf("read %s: %v", hermesConfigPath, err)
	}

	var chains []hermesChain
	var cur *hermesChain
	flush := func() {
		if cur != nil {
			chains = append(chains, *cur)
		}
	}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "[[chains]]":
			flush()
			cur = &hermesChain{line: i + 1}
			continue
		case strings.HasPrefix(line, "[") && line != "[chains.packet_filter]":
			// a top-level table ends the current [[chains]] block
			flush()
			cur = nil
			continue
		}
		if cur == nil || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reChainID.FindStringSubmatch(line); m != nil {
			cur.id = m[1]
		}
		if m := rePrefix.FindStringSubmatch(line); m != nil {
			cur.prefix = m[1]
		}
		if m := reGasPrice.FindStringSubmatch(line); m != nil {
			cur.gasPrice, cur.gasDenom = m[1], m[2]
		}
	}
	flush()

	if len(chains) == 0 {
		t.Fatalf("%s declares no [[chains]] blocks", hermesConfigPath)
	}
	return chains
}

// networksByChainID maps each committed network's CHAIN_ID to its directory
// name, so a hermes block can be matched to the config it must agree with.
func networksByChainID(t *testing.T) map[string]string {
	t.Helper()
	byID := make(map[string]string, len(networks))
	for _, network := range networks {
		env := readChainEnv(t, network)
		id, ok := env["CHAIN_ID"]
		if !ok {
			t.Fatalf("%s/chain.env has no CHAIN_ID", network)
		}
		byID[id] = network
	}
	return byID
}

func TestHermesConfigMatchesChainEnv(t *testing.T) {
	byID := networksByChainID(t)
	ceiling := math.LegacyMustNewDecFromStr(maxSaneGasPrice)

	for _, c := range parseHermesChains(t) {
		t.Run(c.id, func(t *testing.T) {
			if c.id == "" {
				t.Fatalf("%s:%d: [[chains]] block has no id", hermesConfigPath, c.line)
			}
			network, ok := byID[c.id]
			if !ok {
				t.Fatalf("%s:%d: relays for chain id %q, which no network under config/network declares. "+
					"Either the chain id is wrong or the relayer points at a chain this repo does not describe",
					hermesConfigPath, c.line, c.id)
			}
			env := readChainEnv(t, network)

			if c.prefix != bech32Prefix {
				t.Errorf("%s:%d: account_prefix=%q, want %q — hermes would derive addresses the chain rejects",
					hermesConfigPath, c.line, c.prefix, bech32Prefix)
			}

			if got, want := c.gasDenom, env["DENOM"]; got != want {
				t.Errorf("%s:%d: gas_price denom=%q but %s/chain.env DENOM=%q. "+
					"The relayer would offer fees in a denom the chain does not have and every packet it relays would fail",
					hermesConfigPath, c.line, got, network, want)
			}

			// Same units as MIN_GAS_PRICES: a price per gas unit, not a flat
			// fee. A relayer that pays less than the node's minimum has every
			// transaction rejected from the mempool.
			price, err := math.LegacyNewDecFromStr(c.gasPrice)
			if err != nil {
				t.Fatalf("%s:%d: gas_price price=%q does not parse as a decimal: %v",
					hermesConfigPath, c.line, c.gasPrice, err)
			}
			if !price.IsPositive() || price.GTE(ceiling) {
				t.Errorf("%s:%d: gas_price price=%s is not a plausible price per gas unit (ceiling %s)",
					hermesConfigPath, c.line, price, ceiling)
			}
			nodeMin, err := sdk.ParseDecCoins(env["MIN_GAS_PRICES"])
			if err == nil && len(nodeMin) == 1 && price.LT(nodeMin[0].Amount) {
				t.Errorf("%s:%d: gas_price price=%s is below %s/chain.env MIN_GAS_PRICES (%s); "+
					"nodes rendering that app.toml would reject every relayer transaction",
					hermesConfigPath, c.line, price, network, nodeMin[0].Amount)
			}
		})
	}
}

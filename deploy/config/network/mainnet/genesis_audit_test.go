//go:build mainnet

package mainnet_test

import (
	"testing"

	"sparkdream/deploy/config/network/audit"
)

func TestMainnetGenesisHasAllParams(t *testing.T) {
	audit.AssertGenesisParams(t, "genesis.json")
}

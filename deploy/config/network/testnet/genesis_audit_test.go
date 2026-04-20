//go:build testnet

package testnet_test

import (
	"testing"

	"sparkdream/deploy/config/network/audit"
)

func TestTestnetGenesisHasAllParams(t *testing.T) {
	audit.AssertGenesisParams(t, "genesis.json")
}

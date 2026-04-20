//go:build devnet

package devnet_test

import (
	"testing"

	"sparkdream/deploy/config/network/audit"
)

func TestDevnetGenesisHasAllParams(t *testing.T) {
	audit.AssertGenesisParams(t, "genesis.json")
}

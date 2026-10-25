package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// TestMigrationMsgsNotShieldCompatible pins that no migration message can be
// executed anonymously through x/shield: an anonymous proof must never move
// an identity (member-key-migration plan §4).
func TestMigrationMsgsNotShieldCompatible(t *testing.T) {
	f := initFixture(t)
	for _, msg := range types.MigrationMsgs {
		if f.keeper.IsShieldCompatible(f.ctx, msg) {
			t.Errorf("%s is shield-compatible; migration messages must never be", sdk.MsgTypeURL(msg))
		}
	}
}

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// TestIdentityMsgsNotShieldCompatible pins that no identity message (the
// migration messages plus the ZK key messages) can be executed anonymously
// through x/shield: an anonymous proof must never move an identity
// (member-key-migration plan §4) nor re-point the anonymous identity itself
// (multisig-accounts plan §3.2).
func TestIdentityMsgsNotShieldCompatible(t *testing.T) {
	f := initFixture(t)
	for _, msg := range types.IdentityMsgs {
		if f.keeper.IsShieldCompatible(f.ctx, msg) {
			t.Errorf("%s is shield-compatible; identity messages must never be", sdk.MsgTypeURL(msg))
		}
	}
}

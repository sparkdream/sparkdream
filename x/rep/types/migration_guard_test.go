package types_test

import (
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	reptypes "sparkdream/x/rep/types"
	sessiontypes "sparkdream/x/session/types"
)

// migrationMsgNameHints are the name fragments that mark a message as part
// of the member key migration surface. A new message carrying one of them
// that is not in reptypes.MigrationMsgs fails the guard: it would otherwise
// be delegable through a session key or executable through x/shield, and
// neither may ever move an identity (member-key-migration plan §4).
var migrationMsgNameHints = []string{"Migration", "Recovery", "Tombstone"}

// TestMigrationMsgsAreGuarded walks the sparkdream.rep.v1 Msg service and
// checks that every migration message is listed in MigrationMsgs, and that
// every listed message is forbidden from x/session allowlists.
func TestMigrationMsgsAreGuarded(t *testing.T) {
	listed := make(map[string]bool, len(reptypes.MigrationMsgTypeURLs()))
	for _, url := range reptypes.MigrationMsgTypeURLs() {
		listed[url] = true
	}

	var found int
	registry.MergedProtoRegistry().RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != "sparkdream.rep.v1" {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			opts := md.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				continue
			}
			name := string(md.Name())
			isMigration := false
			for _, hint := range migrationMsgNameHints {
				if strings.Contains(name, hint) {
					isMigration = true
				}
			}
			if !isMigration {
				continue
			}
			found++
			url := "/" + string(md.FullName())
			if !listed[url] {
				t.Errorf("%s looks like a migration message but is not in reptypes.MigrationMsgs; add it so x/session and x/shield refuse it", name)
			}
		}
		return true
	})
	if found == 0 {
		t.Fatal("no migration messages found in the rep Msg service; the sweep is broken")
	}
	if found != len(reptypes.MigrationMsgTypeURLs()) {
		t.Errorf("found %d migration messages in the service but MigrationMsgs lists %d", found, len(reptypes.MigrationMsgTypeURLs()))
	}

	for _, url := range reptypes.MigrationMsgTypeURLs() {
		if !sessiontypes.ForbiddenAllowlistMsgs[url] {
			t.Errorf("%s is not in x/session ForbiddenAllowlistMsgs: a session key could move an identity", url)
		}
	}
}

// TestMigrationMsgsNotInSessionDefaults checks that no session allowlist
// default names a migration message.
func TestMigrationMsgsNotInSessionDefaults(t *testing.T) {
	params := sessiontypes.DefaultParams()
	for _, list := range [][]string{params.MaxAllowedMsgTypes, params.AllowedMsgTypes} {
		for _, url := range list {
			if reptypes.IsMigrationMsgTypeURL(url) {
				t.Errorf("x/session default allowlists contain migration message %s", url)
			}
		}
	}
}

// TestIdentityMsgsAreGuarded pins the wider identity surface (multisig-accounts
// plan §3.2): the migration messages plus the ZK key messages. Every one must
// be forbidden from x/session allowlists — a hot key that could register or
// clear a ZK key would bypass a multisig account's threshold on the anonymous
// identity — and absent from the session defaults; params naming one in
// either list must fail validation.
func TestIdentityMsgsAreGuarded(t *testing.T) {
	urls := reptypes.IdentityMsgTypeURLs()
	want := map[string]bool{
		"/sparkdream.rep.v1.MsgRegisterZkPublicKey": true,
		"/sparkdream.rep.v1.MsgClearZkPublicKey":    true,
	}
	for _, u := range reptypes.MigrationMsgTypeURLs() {
		want[u] = true
	}
	if len(urls) != len(want) {
		t.Errorf("IdentityMsgTypeURLs has %d entries, want %d", len(urls), len(want))
	}
	for _, u := range urls {
		if !want[u] {
			t.Errorf("unexpected identity msg %s", u)
		}
		if !sessiontypes.ForbiddenAllowlistMsgs[u] {
			t.Errorf("%s is not in x/session ForbiddenAllowlistMsgs", u)
		}
		if strings.HasPrefix(u, "/sparkdream.rep.v1.Msg") == false {
			t.Errorf("%s is not a rep message", u)
		}
	}

	params := sessiontypes.DefaultParams()
	for _, list := range [][]string{params.MaxAllowedMsgTypes, params.AllowedMsgTypes} {
		for _, u := range list {
			if sessiontypes.ForbiddenAllowlistMsgs[u] {
				t.Errorf("x/session default allowlists contain identity message %s", u)
			}
		}
	}
	for _, u := range urls {
		inCeiling := sessiontypes.DefaultParams()
		inCeiling.MaxAllowedMsgTypes = append(inCeiling.MaxAllowedMsgTypes, u)
		if err := inCeiling.Validate(); err == nil {
			t.Errorf("params accepted %s in max_allowed_msg_types", u)
		}
		inActive := sessiontypes.DefaultParams()
		inActive.MaxAllowedMsgTypes = append(inActive.MaxAllowedMsgTypes, u)
		inActive.AllowedMsgTypes = append(inActive.AllowedMsgTypes, u)
		if err := inActive.Validate(); err == nil {
			t.Errorf("params accepted %s in allowed_msg_types", u)
		}
	}
}

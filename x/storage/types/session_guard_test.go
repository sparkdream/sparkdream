package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	sessiontypes "sparkdream/x/session/types"
	storagetypes "sparkdream/x/storage/types"
)

// TestEveryStorageMsgHasASessionDecision walks the sparkdream.storage.v1 Msg
// service. Every signer message is either delegable (in both x/session
// default allowlists: operator and scanner workers sign with a session key)
// or non-delegable (in x/session's ForbiddenAllowlistMsgs: it moves a
// member's funds, or is a council or governance decision). A new message
// without a decision fails here.
func TestEveryStorageMsgHasASessionDecision(t *testing.T) {
	delegable := map[string]bool{}
	for _, u := range storagetypes.DelegableMsgTypeURLs() {
		delegable[u] = true
	}
	forbidden := map[string]bool{}
	for _, u := range storagetypes.NonDelegableMsgTypeURLs() {
		if delegable[u] {
			t.Errorf("%s is listed as both delegable and non-delegable", u)
		}
		forbidden[u] = true
	}
	inList := func(list []string, url string) bool {
		for _, a := range list {
			if a == url {
				return true
			}
		}
		return false
	}
	defaults := sessiontypes.DefaultParams()
	found := 0
	registry.MergedProtoRegistry().RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != "sparkdream.storage.v1" {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			opts := md.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				continue
			}
			found++
			url := "/" + string(md.FullName())
			switch {
			case delegable[url]:
				if sessiontypes.ForbiddenAllowlistMsgs[url] {
					t.Errorf("%s is delegable but in ForbiddenAllowlistMsgs", url)
				}
				if !inList(defaults.MaxAllowedMsgTypes, url) || !inList(defaults.AllowedMsgTypes, url) {
					t.Errorf("%s is delegable but missing from a session default allowlist", url)
				}
			case forbidden[url]:
				if !sessiontypes.ForbiddenAllowlistMsgs[url] {
					t.Errorf("%s is not in x/session ForbiddenAllowlistMsgs", url)
				}
				if inList(defaults.MaxAllowedMsgTypes, url) || inList(defaults.AllowedMsgTypes, url) {
					t.Errorf("%s appears in a session default allowlist", url)
				}
			default:
				t.Errorf("%s has no session decision: add it to DelegableMsgTypeURLs or NonDelegableMsgTypeURLs", url)
			}
		}
		return true
	})
	if found == 0 {
		t.Fatal("no storage messages found; the sweep is broken")
	}
	if found != len(delegable)+len(forbidden) {
		t.Errorf("service has %d signer messages, the lists hold %d", found, len(delegable)+len(forbidden))
	}
	// Every council message is a non-delegable one.
	for _, u := range storagetypes.CouncilMsgTypeURLs() {
		if !forbidden[u] {
			t.Errorf("council message %s must be non-delegable", u)
		}
	}
}

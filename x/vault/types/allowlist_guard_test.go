package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	sessiontypes "sparkdream/x/session/types"
	vaulttypes "sparkdream/x/vault/types"
)

// TestVaultMsgsAreNeverDelegable walks the sparkdream.vault.v1 Msg service:
// every signer message is in NonDelegableMsgs and in (amino names are covered
// by x/collect's service-walking amino_name_test)
// x/session's ForbiddenAllowlistMsgs (a hot key must never cast a co-signer's
// approval). Only MsgUpdateVault and MsgCancelProposal may run as inner
// messages of a vault proposal.
func TestVaultMsgsAreNeverDelegable(t *testing.T) {
	listed := map[string]bool{}
	for _, u := range vaulttypes.NonDelegableMsgTypeURLs() {
		listed[u] = true
	}
	found := 0
	registry.MergedProtoRegistry().RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != "sparkdream.vault.v1" {
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
			if !listed[url] {
				t.Errorf("%s is not in vaulttypes.NonDelegableMsgs", url)
			}
			if !sessiontypes.ForbiddenAllowlistMsgs[url] {
				t.Errorf("%s is not in x/session ForbiddenAllowlistMsgs", url)
			}
			for _, list := range [][]string{sessiontypes.DefaultParams().MaxAllowedMsgTypes, sessiontypes.DefaultParams().AllowedMsgTypes} {
				for _, a := range list {
					if a == url {
						t.Errorf("%s appears in a session default allowlist", url)
					}
				}
			}
		}
		return true
	})
	if found == 0 {
		t.Fatal("no vault messages found; the sweep is broken")
	}
	if found != len(listed) {
		t.Errorf("service has %d signer messages, NonDelegableMsgs lists %d", found, len(listed))
	}
	allowed := 0
	for u := range listed {
		if vaulttypes.IsInnerAllowedVaultMsg(u) {
			allowed++
		}
	}
	if allowed != 2 {
		t.Errorf("exactly MsgUpdateVault and MsgCancelProposal may be inner messages, got %d allowed", allowed)
	}
}

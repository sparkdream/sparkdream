package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/reflect/protoreflect"

	"sparkdream/x/session/types"
)

// TestEverySessionMsgIsNonDelegable walks the x/session Msg service and
// fails when a signer Msg is missing from NonDelegableSessionMsgs, so a
// newly scaffolded session message defaults to denylisted. MsgRevokeGrant
// is the one deliberate exception (gated by allow_self_revoke).
func TestEverySessionMsgIsNonDelegable(t *testing.T) {
	desc, err := registry.MergedProtoRegistry().FindDescriptorByName(protoreflect.FullName("sparkdream.session.v1.Msg"))
	if err != nil {
		t.Fatalf("Msg service descriptor not found: %v", err)
	}
	methods := desc.(protoreflect.ServiceDescriptor).Methods()
	for i := 0; i < methods.Len(); i++ {
		url := "/" + string(methods.Get(i).Input().FullName())
		if url == "/sparkdream.session.v1.MsgRevokeGrant" {
			if types.NonDelegableSessionMsgs[url] {
				t.Fatalf("%s is intentionally delegable (allow_self_revoke)", url)
			}
			continue
		}
		if !types.NonDelegableSessionMsgs[url] {
			t.Errorf("%s is not in NonDelegableSessionMsgs", url)
		}
	}
}

// Theme 6: messages guarded by an ante-only rule never enter an allowlist.
func TestAnteOnlyRuleMsgsForbidden(t *testing.T) {
	for _, url := range []string{
		"/sparkdream.commons.v1.MsgSubmitProposal",
		"/gnovm.gnovm.v1.MsgRun",
		"/gnovm.gnovm.v1.MsgCall",
		"/gnovm.gnovm.v1.MsgAddPackage",
	} {
		if !types.ForbiddenAllowlistMsgs[url] {
			t.Errorf("%s must be in ForbiddenAllowlistMsgs", url)
		}
	}
}

// A bridge operator's identity attestation is signed by the operator key,
// never a daemon session key.
func TestFederationAttestationForbidden(t *testing.T) {
	if !types.ForbiddenAllowlistMsgs["/sparkdream.federation.v1.MsgAttestIdentityLink"] {
		t.Error("/sparkdream.federation.v1.MsgAttestIdentityLink must be in ForbiddenAllowlistMsgs")
	}
}

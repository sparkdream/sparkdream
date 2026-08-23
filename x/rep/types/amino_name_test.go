package types_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	aminoapi "cosmossdk.io/api/amino"
	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	// import to register descriptors via init().
	_ "sparkdream/x/rep/types"
)

// TestAminoNamesPresent asserts that every x/rep message a user can sign carries
// a stable (amino.name) option on its proto descriptor.
//
// Without the annotation, the SDK's aminojson sign-mode handler
// (cosmossdk.io/x/tx/signing/aminojson) cannot build a SIGN_MODE_LEGACY_AMINO_JSON
// sign-doc, and Keplr+Ledger users — who have no other sign mode available —
// get "signature verification failed" with nothing pointing at the cause.
//
// Unlike the equivalent guard in x/commons, this one discovers its own subjects:
// it walks every message in the sparkdream.rep.v1 package and tests the ones
// carrying cosmos.msg.v1.signer. x/rep has over sixty signer messages and gains
// them regularly, so a hand-maintained list would fail exactly the way the thing
// it guards fails — silently, by omission, the moment someone forgets to add a
// row. A new signer Msg is covered the moment it is scaffolded.
func TestAminoNamesPresent(t *testing.T) {
	const (
		pkg    = "sparkdream.rep.v1"
		prefix = "sparkdream/x/rep/"
	)

	var signerMsgs []protoreflect.MessageDescriptor

	registry.MergedProtoRegistry().RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != pkg {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			opts := md.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				continue
			}
			if signers := proto.GetExtension(opts, msgv1.E_Signer).([]string); len(signers) == 0 {
				continue
			}
			signerMsgs = append(signerMsgs, md)
		}
		return true
	})

	// Guard the guard: if descriptor registration ever changes shape, an empty
	// sweep would make this test pass while checking nothing.
	if len(signerMsgs) == 0 {
		t.Fatalf("no %s messages with cosmos.msg.v1.signer found — the sweep is broken, not the protos", pkg)
	}

	for _, md := range signerMsgs {
		name := string(md.Name())
		t.Run(name, func(t *testing.T) {
			opts := md.Options()
			if !proto.HasExtension(opts, aminoapi.E_Name) {
				t.Fatalf("amino.name not set on %s.%s — Ledger signing will fail for this message.\n"+
					"Add: option (amino.name) = %q; (and import \"amino/amino.proto\")",
					pkg, name, prefix+name)
			}
			got := proto.GetExtension(opts, aminoapi.E_Name).(string)

			// The name is what Ledger hashes, so it has to be stable and it has
			// to be unambiguous across modules. Pinning the exact derivation
			// keeps a typo'd or copy-pasted name from shipping.
			want := prefix + name
			if got != want {
				t.Fatalf("amino.name mismatch on %s.%s: got %q, want %q", pkg, name, got, want)
			}
			if !strings.HasPrefix(got, prefix) {
				t.Fatalf("amino.name %q on %s.%s does not carry the module prefix %q",
					got, pkg, name, prefix)
			}
		})
	}

	// Duplicate names would collide in the aminojson handler's lookup, so two
	// messages sharing one is a signing bug even when both are individually set.
	seen := make(map[string]string, len(signerMsgs))
	for _, md := range signerMsgs {
		opts := md.Options()
		if !proto.HasExtension(opts, aminoapi.E_Name) {
			continue
		}
		got := proto.GetExtension(opts, aminoapi.E_Name).(string)
		if prev, dup := seen[got]; dup {
			t.Fatalf("amino.name %q is used by both %s and %s", got, prev, md.Name())
		}
		seen[got] = string(md.Name())
	}

	t.Log(fmt.Sprintf("checked %d signer messages in %s", len(signerMsgs), pkg))
}

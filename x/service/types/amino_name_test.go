package types_test

import (
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	aminoapi "cosmossdk.io/api/amino"
	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	// import to register descriptors via init().
	_ "sparkdream/x/service/types"
)

// TestAminoNamesPresent asserts that every signer Msg in the service Msg
// service has a stable (amino.name) option set on its proto descriptor.
// Sibling of the same guard in x/collect/types — see x/commons/types for
// the full rationale (Keplr+Ledger only supports amino-JSON signing; a
// missing annotation fails with "signature verification failed").
//
// Walks the Msg service descriptor, so newly scaffolded service messages
// are covered without editing a case list.
func TestAminoNamesPresent(t *testing.T) {
	resolver := registry.MergedProtoRegistry()

	desc, err := resolver.FindDescriptorByName(protoreflect.FullName("sparkdream.service.v1.Msg"))
	if err != nil {
		t.Fatalf("Msg service descriptor not found: %v", err)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("descriptor is not a service: %T", desc)
	}

	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		input := methods.Get(i).Input()
		t.Run(string(input.FullName()), func(t *testing.T) {
			opts := input.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				t.Fatalf("cosmos.msg.v1.signer not set on %s", input.FullName())
			}
			if !proto.HasExtension(opts, aminoapi.E_Name) {
				t.Fatalf("amino.name not set on %s — Ledger signing will fail", input.FullName())
			}
			got := proto.GetExtension(opts, aminoapi.E_Name).(string)
			want := "sparkdream/x/service/" + string(input.Name())
			if got != want {
				t.Fatalf("amino.name mismatch for %s: got %q, want %q", input.FullName(), got, want)
			}
			if strings.TrimSpace(got) == "" {
				t.Fatalf("amino.name empty on %s", input.FullName())
			}
		})
	}
}

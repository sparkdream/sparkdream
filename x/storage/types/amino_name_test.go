package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	aminoapi "cosmossdk.io/api/amino"
	msgv1 "cosmossdk.io/api/cosmos/msg/v1"

	// import to register descriptors via init().
	_ "sparkdream/x/storage/types"
)

// TestAminoNamesPresent walks the sparkdream.storage.v1 Msg service and
// fails for a signer message without its (amino.name) option. Keplr with a
// Ledger signs amino-JSON only, and a message without the name fails there
// with "signature verification failed" (docs/development-conventions.md,
// amino names). A new message is covered without editing a list.
func TestAminoNamesPresent(t *testing.T) {
	desc, err := registry.MergedProtoRegistry().FindDescriptorByName(protoreflect.FullName("sparkdream.storage.v1.Msg"))
	if err != nil {
		t.Fatalf("Msg service descriptor not found: %v", err)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("descriptor is not a service: %T", desc)
	}
	methods := svc.Methods()
	if methods.Len() == 0 {
		t.Fatal("the Msg service has no methods")
	}
	for i := 0; i < methods.Len(); i++ {
		input := methods.Get(i).Input()
		t.Run(string(input.FullName()), func(t *testing.T) {
			opts := input.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				t.Fatalf("cosmos.msg.v1.signer not set on %s", input.FullName())
			}
			if !proto.HasExtension(opts, aminoapi.E_Name) {
				t.Fatalf("amino.name not set on %s: Ledger signing will fail", input.FullName())
			}
			got := proto.GetExtension(opts, aminoapi.E_Name).(string)
			if want := "sparkdream/x/storage/" + string(input.Name()); got != want {
				t.Fatalf("amino.name of %s: got %q, want %q", input.FullName(), got, want)
			}
		})
	}
}

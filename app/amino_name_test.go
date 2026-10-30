package app

import (
	"strings"
	"testing"

	aminoapi "cosmossdk.io/api/amino"
	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestAminoNamesPresentAllModules walks every `sparkdream.<module>.v1.Msg`
// service in the merged proto registry and asserts that each signer Msg
// carries `(amino.name) = "sparkdream/x/<module>/<MsgName>"`. Without it,
// Keplr+Ledger users (amino-JSON signing only) get "signature verification
// failed". The app package imports every module, so a new module or a new
// message is covered without editing a case list (the per-module guards
// in x/*/types remain as faster local checks).
func TestAminoNamesPresentAllModules(t *testing.T) {
	files := registry.MergedProtoRegistry()
	services := 0
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		pkg := string(fd.Package())
		parts := strings.Split(pkg, ".")
		if len(parts) != 3 || parts[0] != "sparkdream" {
			return true
		}
		svc := fd.Services().ByName("Msg")
		if svc == nil {
			return true
		}
		services++
		module := parts[1]
		methods := svc.Methods()
		for i := 0; i < methods.Len(); i++ {
			input := methods.Get(i).Input()
			opts := input.Options()
			if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
				t.Errorf("cosmos.msg.v1.signer not set on %s", input.FullName())
				continue
			}
			if !proto.HasExtension(opts, aminoapi.E_Name) {
				t.Errorf("amino.name not set on %s — Ledger signing will fail", input.FullName())
				continue
			}
			got := proto.GetExtension(opts, aminoapi.E_Name).(string)
			want := "sparkdream/x/" + module + "/" + string(input.Name())
			if got != want {
				t.Errorf("amino.name mismatch for %s: got %q, want %q", input.FullName(), got, want)
			}
		}
		return true
	})
	if services < 15 {
		t.Fatalf("only %d sparkdream Msg services found; registry not loaded?", services)
	}
}

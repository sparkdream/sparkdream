package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	aminoapi "cosmossdk.io/api/amino"

	// import to register descriptors via init().
	_ "sparkdream/x/commons/types"
)

// TestAminoNamesPresent asserts that every Msg* type a Ledger user can sign
// has a stable (amino.name) option set on its proto descriptor.
//
// Without these annotations, Keplr+Ledger transactions fail with
// "signature verification failed" because the SDK's aminojson sign-mode handler
// (cosmossdk.io/x/tx/signing/aminojson) cannot produce a canonical sign-doc.
//
// See discussion in proto/sparkdream/commons/v1/tx.proto and the related fix
// across all module tx.protos.
func TestAminoNamesPresent(t *testing.T) {
	cases := []struct {
		fullName string
		expected string
	}{
		{"sparkdream.commons.v1.MsgSubmitProposal", "sparkdream/x/commons/MsgSubmitProposal"},
		{"sparkdream.commons.v1.MsgVoteProposal", "sparkdream/x/commons/MsgVoteProposal"},
		{"sparkdream.commons.v1.MsgExecuteProposal", "sparkdream/x/commons/MsgExecuteProposal"},
		{"sparkdream.commons.v1.MsgUpdateGroupMembers", "sparkdream/x/commons/MsgUpdateGroupMembers"},
		{"sparkdream.commons.v1.MsgUpdateGroupConfig", "sparkdream/x/commons/MsgUpdateGroupConfig"},
		{"sparkdream.commons.v1.MsgRegisterGroup", "sparkdream/x/commons/MsgRegisterGroup"},
		{"sparkdream.commons.v1.MsgRenewGroup", "sparkdream/x/commons/MsgRenewGroup"},
		{"sparkdream.commons.v1.MsgDeleteGroup", "sparkdream/x/commons/MsgDeleteGroup"},
		{"sparkdream.commons.v1.MsgVetoGroupProposals", "sparkdream/x/commons/MsgVetoGroupProposals"},
		{"sparkdream.commons.v1.MsgForceUpgrade", "sparkdream/x/commons/MsgForceUpgrade"},
		{"sparkdream.commons.v1.MsgCreatePolicyPermissions", "sparkdream/x/commons/MsgCreatePolicyPermissions"},
		{"sparkdream.commons.v1.MsgUpdatePolicyPermissions", "sparkdream/x/commons/MsgUpdatePolicyPermissions"},
		{"sparkdream.commons.v1.MsgDeletePolicyPermissions", "sparkdream/x/commons/MsgDeletePolicyPermissions"},
		{"sparkdream.commons.v1.MsgCreateCategory", "sparkdream/x/commons/MsgCreateCategory"},
	}

	resolver := registry.MergedProtoRegistry()
	for _, tc := range cases {
		t.Run(tc.fullName, func(t *testing.T) {
			desc, err := resolver.FindDescriptorByName(protoreflect.FullName(tc.fullName))
			if err != nil {
				t.Fatalf("descriptor not found: %v", err)
			}
			msgDesc, ok := desc.(interface{ Options() proto.Message })
			if !ok {
				t.Fatalf("descriptor for %s does not expose Options()", tc.fullName)
			}
			opts := msgDesc.Options()
			if opts == nil || !proto.HasExtension(opts, aminoapi.E_Name) {
				t.Fatalf("amino.name not set on %s — Ledger signing will fail", tc.fullName)
			}
			got := proto.GetExtension(opts, aminoapi.E_Name).(string)
			if got != tc.expected {
				t.Fatalf("amino.name mismatch for %s: got %q, want %q", tc.fullName, got, tc.expected)
			}
		})
	}
}

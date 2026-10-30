package keeper

import (
	"bytes"
	"fmt"
	"reflect"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// signerFieldName returns the name of the single top-level signer field of
// msg, read from its `cosmos.msg.v1.signer` proto option: the same source the
// SDK uses to derive signers, so the rewrite can never pick a decoy field
// that merely shares a conventional name. Nested signer paths and
// multi-signer messages are refused.
func signerFieldName(msg sdk.Msg) (string, error) {
	name := gogoproto.MessageName(msg)
	if name == "" {
		return "", fmt.Errorf("%T has no registered proto name", msg)
	}
	desc, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return "", fmt.Errorf("descriptor for %s: %w", name, err)
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return "", fmt.Errorf("%s is not a message", name)
	}
	opts := md.Options()
	if opts == nil || !proto.HasExtension(opts, msgv1.E_Signer) {
		return "", fmt.Errorf("%s has no cosmos.msg.v1.signer option", name)
	}
	signers, _ := proto.GetExtension(opts, msgv1.E_Signer).([]string)
	if len(signers) != 1 {
		return "", fmt.Errorf("%s declares %d signer fields, expected 1", name, len(signers))
	}
	fd := md.Fields().ByName(protoreflect.Name(signers[0]))
	if fd == nil || fd.Kind() != protoreflect.StringKind || fd.IsList() {
		return "", fmt.Errorf("%s signer field %q is not a plain string field", name, signers[0])
	}
	return signers[0], nil
}

// rewriteSignerField sets msg's signer field (resolved from the proto
// signer option) to granter.
func rewriteSignerField(msg sdk.Msg, granter string) error {
	field, err := signerFieldName(msg)
	if err != nil {
		return err
	}
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("message is not a struct")
	}
	f := v.FieldByName(snakeToPascal(field))
	if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
		return fmt.Errorf("signer field %q not settable on %T", field, msg)
	}
	f.SetString(granter)
	return nil
}

// rewriteAndCheckSigner rewrites msg's signer to granter and then asserts,
// through the codec's own signer derivation, that the message now has
// exactly one signer and that it is the granter. Any mismatch fails closed.
func rewriteAndCheckSigner(cdc codec.Codec, msg sdk.Msg, granter string, granterBz []byte) error {
	if err := rewriteSignerField(msg, granter); err != nil {
		return err
	}
	pc, ok := cdc.(*codec.ProtoCodec)
	if !ok {
		return fmt.Errorf("codec is not ProtoCodec")
	}
	signers, _, err := pc.GetMsgV1Signers(msg)
	if err != nil {
		return fmt.Errorf("cannot determine signers: %w", err)
	}
	if len(signers) != 1 || !bytes.Equal(signers[0], granterBz) {
		return fmt.Errorf("signer after rewrite is not the granter")
	}
	return nil
}

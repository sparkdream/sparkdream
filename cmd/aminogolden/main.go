// aminogolden exports what a client needs to sign sparkdream messages in
// amino-JSON mode (Keplr+Ledger) exactly as the chain renders them:
//
//	aminogolden schema   descriptors of every sparkdream Msg (and the cosmos
//	                     messages clients wrap them in), with the field options
//	                     cosmossdk.io/x/tx/signing/aminojson acts on
//	aminogolden golden   sample messages as protobuf bytes next to the amino
//	                     JSON the chain's aminojson encoder renders for them
//	aminogolden verify   reads amino-signed transactions (JSONL on stdin) and
//	                     verifies each signature as the ante handler does, from
//	                     sign bytes the SDK's aminojson sign mode rebuilds
//	                     out of the decoded tx body
//
// sparkdreamjs builds its amino converters from the schema
// (src-overrides/chain-amino.ts) and checks them against the golden records,
// and sparkdream-ui checks its signing setup against them too:
//
//	go run ./cmd/aminogolden schema | node <sparkdreamjs>/packages/sparkdreamjs/scripts/amino-schema.js
//	go run ./cmd/aminogolden golden > <sparkdreamjs>/packages/sparkdreamjs/__tests__/amino-golden.jsonl
//	go run ./cmd/aminogolden golden > ../sparkdream-ui/src/lib/__fixtures__/amino-golden.jsonl
//
// Rerun both after any proto change. The UI's signing test can write the
// transactions it signs for verify (see AMINO_SIGNED_OUT in
// src/lib/aminoSigning.test.ts).
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	aminoapi "cosmossdk.io/api/amino"
	secp256k1api "cosmossdk.io/api/cosmos/crypto/secp256k1"
	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	txdecode "cosmossdk.io/x/tx/decode"
	txsigning "cosmossdk.io/x/tx/signing"
	"cosmossdk.io/x/tx/signing/aminojson"
	cosmos_proto "github.com/cosmos/cosmos-proto"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/iancoleman/strcase"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"

	_ "sparkdream/app"
)

var (
	files *protoregistry.Files
	types *dynamicpb.Types
	enc   aminojson.Encoder
	// Values for Any fields: a cosmos message, and a sparkdream message with
	// repeated, uint64 and Dec fields of its own.
	anyBank       []byte
	anySparkURL   = "/sparkdream.commons.v1.MsgUpdateGroupConfig"
	anySparkdream []byte
)

// cosmos messages clients sign directly or wrap sparkdream messages in.
var extraMsgs = map[protoreflect.FullName]bool{
	"cosmos.gov.v1.MsgSubmitProposal":           true,
	"cosmos.upgrade.v1beta1.MsgSoftwareUpgrade": true,
	"cosmos.upgrade.v1beta1.MsgCancelUpgrade":   true,
}

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "schema" && os.Args[1] != "golden" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "usage: aminogolden schema|golden|verify")
		os.Exit(2)
	}
	var err error
	files, err = gogoproto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	types = dynamicpb.NewTypes(files)
	enc = aminojson.NewEncoder(aminojson.EncoderOptions{FileResolver: files, TypeResolver: types})

	if os.Args[1] == "verify" {
		os.Exit(verify())
	}
	msgs := msgTypes()
	if os.Args[1] == "schema" {
		writeSchema(msgs)
		return
	}

	send := &banktypes.MsgSend{FromAddress: "from-v", ToAddress: "to-v", Amount: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5)))}
	packed, err := codectypes.NewAnyWithValue(send)
	if err != nil {
		panic(err)
	}
	anyBank = packed.Value
	desc, err := files.FindDescriptorByName(protoreflect.FullName(anySparkURL[1:]))
	if err != nil {
		panic(err)
	}
	spark := dynamicpb.NewMessage(desc.(protoreflect.MessageDescriptor))
	messageValue(spark, 0, full)
	if anySparkdream, err = (proto.MarshalOptions{Deterministic: true}).Marshal(spark); err != nil {
		panic(err)
	}

	out := json.NewEncoder(os.Stdout)
	for _, md := range msgs {
		for _, v := range variants {
			emit(out, md, v)
		}
	}
}

// msgTypes lists every sparkdream Msg service input plus extraMsgs, sorted.
func msgTypes() []protoreflect.MessageDescriptor {
	var out []protoreflect.MessageDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			if sd.Name() != "Msg" {
				continue
			}
			methods := sd.Methods()
			for j := 0; j < methods.Len(); j++ {
				in := methods.Get(j).Input()
				if strings.HasPrefix(string(fd.Package()), "sparkdream.") || extraMsgs[in.FullName()] {
					out = append(out, in)
				}
			}
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FullName() < out[j].FullName() })
	return out
}

// ---------------------------------------------------------------- options

func fieldOpts(fd protoreflect.FieldDescriptor) *descriptorpb.FieldOptions {
	opts, _ := fd.Options().(*descriptorpb.FieldOptions)
	return opts
}

func stringExt(opts proto.Message, xt protoreflect.ExtensionType) string {
	if opts == nil || !proto.HasExtension(opts, xt) {
		return ""
	}
	return proto.GetExtension(opts, xt).(string)
}

func scalarOf(fd protoreflect.FieldDescriptor) string {
	return stringExt(fieldOpts(fd), cosmos_proto.E_Scalar)
}

func aminoEncoding(fd protoreflect.FieldDescriptor) string {
	return stringExt(fieldOpts(fd), aminoapi.E_Encoding)
}

func dontOmitEmpty(fd protoreflect.FieldDescriptor) bool {
	opts := fieldOpts(fd)
	return opts != nil && proto.HasExtension(opts, aminoapi.E_DontOmitempty) && proto.GetExtension(opts, aminoapi.E_DontOmitempty).(bool)
}

func aminoFieldName(fd protoreflect.FieldDescriptor) string {
	if n := stringExt(fieldOpts(fd), aminoapi.E_FieldName); n != "" {
		return n
	}
	return string(fd.Name())
}

func messageAminoName(md protoreflect.MessageDescriptor) string {
	return stringExt(md.Options(), aminoapi.E_Name)
}

func messageEncoding(md protoreflect.MessageDescriptor) string {
	return stringExt(md.Options(), aminoapi.E_MessageEncoding)
}

// gogoOption returns the raw value of a gogoproto field option, a gogo
// extension the new API leaves in the unknown fields.
func gogoOption(fd protoreflect.FieldDescriptor, want protowire.Number) (uint64, []byte, bool) {
	opts := fieldOpts(fd)
	if opts == nil {
		return 0, nil, false
	}
	b := opts.ProtoReflect().GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return 0, nil, false
		}
		b = b[n:]
		if num == want {
			switch typ {
			case protowire.VarintType:
				v, m := protowire.ConsumeVarint(b)
				return v, nil, m >= 0
			case protowire.BytesType:
				v, m := protowire.ConsumeBytes(b)
				return 0, v, m >= 0
			}
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return 0, nil, false
		}
		b = b[m:]
	}
	return 0, nil, false
}

// nonNullable reports gogoproto.nullable = false: gogo always serializes
// such an embedded message, present even when empty.
func nonNullable(fd protoreflect.FieldDescriptor) bool {
	v, _, ok := gogoOption(fd, 65001)
	return ok && v == 0
}

// customType returns gogoproto.customtype, e.g. "cosmossdk.io/math.LegacyDec".
// Unlike nullable it resolves as a registered extension (aminojson registers
// it, since it needs it too).
func customType(fd protoreflect.FieldDescriptor) string {
	opts := fieldOpts(fd)
	if opts == nil {
		return ""
	}
	out := ""
	proto.RangeExtensions(opts, func(xt protoreflect.ExtensionType, v interface{}) bool {
		if xt.TypeDescriptor().FullName() == "gogoproto.customtype" {
			out, _ = v.(string)
			return false
		}
		return true
	})
	if out == "" {
		_, b, _ := gogoOption(fd, 65003)
		out = string(b)
	}
	return out
}

// ----------------------------------------------------------------- schema

// schemaField mirrors what aminojson's marshalMessage reads off a field.
type schemaField struct {
	Name     string     `json:"n"`            // amino field name
	Number   int32      `json:"no"`           // field number
	Kind     string     `json:"k"`            // protoreflect kind, or "map"
	Repeated bool       `json:"r,omitempty"`  // repeated
	Presence bool       `json:"pr,omitempty"` // proto3 optional (explicit presence)
	DontOmit bool       `json:"d,omitempty"`  // amino.dont_omitempty
	Scalar   string     `json:"sc,omitempty"` // "int" (cosmos.Int) or "dec" (cosmos.Dec on a LegacyDec)
	Encoding string     `json:"e,omitempty"`  // amino.encoding
	Type     string     `json:"t,omitempty"`  // message type full name
	JSONName string     `json:"j,omitempty"`  // protobuf JSON name of a message field (the client's key)
	NonNull  bool       `json:"nn,omitempty"` // gogoproto.nullable = false on a message field
	Oneof    *[2]string `json:"o,omitempty"`  // [oneof JSON name, amino.oneof_name] for a real oneof member
}

type schemaMessage struct {
	AminoName string        `json:"a,omitempty"`
	Encoding  string        `json:"me,omitempty"`
	Fields    []schemaField `json:"f"`
}

type schema struct {
	Msgs     []string                 `json:"msgs"`
	Messages map[string]schemaMessage `json:"messages"`
}

// Well-known types aminojson renders with dedicated encoders.
var builtin = map[protoreflect.FullName]bool{
	"google.protobuf.Any": true, "google.protobuf.Timestamp": true, "google.protobuf.Duration": true,
}

func writeSchema(msgs []protoreflect.MessageDescriptor) {
	s := schema{Messages: map[string]schemaMessage{}}
	var add func(md protoreflect.MessageDescriptor)
	add = func(md protoreflect.MessageDescriptor) {
		name := string(md.FullName())
		if builtin[md.FullName()] {
			return
		}
		if _, ok := s.Messages[name]; ok {
			return
		}
		sm := schemaMessage{AminoName: messageAminoName(md), Encoding: messageEncoding(md), Fields: []schemaField{}}
		s.Messages[name] = sm // placeholder breaks recursion
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			f := schemaField{
				Name:     aminoFieldName(fd),
				Number:   int32(fd.Number()),
				Kind:     fd.Kind().String(),
				Repeated: fd.IsList(),
				Presence: fd.HasPresence() && fd.Kind() != protoreflect.MessageKind && fd.ContainingOneof() == nil,
				DontOmit: dontOmitEmpty(fd),
				Encoding: aminoEncoding(fd),
			}
			if fd.IsMap() {
				f.Kind = "map"
				f.Repeated = false
			}
			switch scalarOf(fd) {
			case "cosmos.Int":
				f.Scalar = "int"
			case "cosmos.Dec":
				// aminojson formats a cosmos.Dec only on a LegacyDec customtype.
				if customType(fd) == "cosmossdk.io/math.LegacyDec" {
					f.Scalar = "dec"
				}
			}
			if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
				f.Oneof = &[2]string{strcase.ToCamel(string(od.Name())), stringExt(fieldOpts(fd), aminoapi.E_OneofName)}
			}
			if fd.Kind() == protoreflect.MessageKind && !fd.IsMap() {
				f.Type = string(fd.Message().FullName())
				f.JSONName = fd.JSONName()
				f.NonNull = nonNullable(fd)
				add(fd.Message())
			}
			sm.Fields = append(sm.Fields, f)
		}
		s.Messages[name] = sm
	}
	for _, md := range msgs {
		s.Msgs = append(s.Msgs, string(md.FullName()))
		add(md)
	}
	// The cosmos messages an Any in a sparkdream message commonly carries.
	for _, n := range []protoreflect.FullName{"cosmos.bank.v1beta1.MsgSend"} {
		d, err := files.FindDescriptorByName(n)
		if err != nil {
			panic(err)
		}
		add(d.(protoreflect.MessageDescriptor))
	}
	out := json.NewEncoder(os.Stdout)
	if err := out.Encode(s); err != nil {
		panic(err)
	}
}

// ----------------------------------------------------------------- golden

type variant int

const (
	zero         variant = iota // gogo's zero value: non-nullable messages present but empty
	full                        // every field set, one element per list
	emptyLists                  // every field set except lists and maps
	presentEmpty                // every singular message present but empty, scalars zero
	negatives                   // full, with signed integers negative
)

var variants = []variant{zero, full, emptyLists, presentEmpty, negatives}
var variantNames = []string{"zero", "full", "empty_lists", "present_empty", "negatives"}

func scalarValue(fd protoreflect.FieldDescriptor, v variant) protoreflect.Value {
	sign := int64(1)
	if v == negatives {
		sign = -1
	}
	switch fd.Kind() {
	case protoreflect.StringKind:
		ct := customType(fd)
		switch {
		case strings.HasSuffix(ct, "Dec") || scalarOf(fd) == "cosmos.Dec":
			// LegacyDec travels as its 18-decimal integer representation.
			return protoreflect.ValueOfString("1500000000000000000")
		case strings.HasSuffix(ct, "Int") || scalarOf(fd) == "cosmos.Int":
			return protoreflect.ValueOfString("5")
		}
		return protoreflect.ValueOfString(string(fd.Name()) + "-v")
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(true)
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(int32(7 * sign)) //nolint:gosec // sign is -1 or 1
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(7)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(7 * sign)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(7)
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(1.5)
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(1.5)
	case protoreflect.BytesKind:
		if scalarOf(fd) == "cosmos.Dec" || strings.HasSuffix(customType(fd), "Dec") {
			return protoreflect.ValueOfBytes([]byte("1500000000000000000"))
		}
		return protoreflect.ValueOfBytes([]byte{1, 2, 3})
	case protoreflect.EnumKind:
		vals := fd.Enum().Values()
		if vals.Len() > 1 {
			return protoreflect.ValueOfEnum(vals.Get(1).Number())
		}
		return protoreflect.ValueOfEnum(0)
	}
	panic(fmt.Sprintf("unhandled kind %v on %s", fd.Kind(), fd.FullName()))
}

func messageValue(m protoreflect.Message, depth int, v variant) {
	md := m.Descriptor()
	switch md.FullName() {
	case "google.protobuf.Any":
		if v == presentEmpty {
			return
		}
		url, value := "/cosmos.bank.v1beta1.MsgSend", anyBank
		if v == full && anySparkdream != nil {
			url, value = anySparkURL, anySparkdream
		}
		m.Set(md.Fields().ByName("type_url"), protoreflect.ValueOfString(url))
		m.Set(md.Fields().ByName("value"), protoreflect.ValueOfBytes(value))
		return
	case "google.protobuf.Timestamp":
		if v != presentEmpty {
			m.Set(md.Fields().ByName("seconds"), protoreflect.ValueOfInt64(1_700_000_000))
			// Half a second: clients hold timestamps as millisecond Dates.
			m.Set(md.Fields().ByName("nanos"), protoreflect.ValueOfInt32(500_000_000))
		}
		return
	case "google.protobuf.Duration":
		if v != presentEmpty {
			m.Set(md.Fields().ByName("seconds"), protoreflect.ValueOfInt64(60))
		}
		return
	case "cosmos.base.v1beta1.Coin":
		if v != presentEmpty {
			m.Set(md.Fields().ByName("denom"), protoreflect.ValueOfString("uspark"))
			m.Set(md.Fields().ByName("amount"), protoreflect.ValueOfString("5"))
		}
		return
	}
	if depth > 4 {
		return
	}
	seenOneof := map[protoreflect.FullName]bool{}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
			if seenOneof[od.FullName()] {
				continue
			}
			seenOneof[od.FullName()] = true
		}
		switch {
		case fd.IsMap():
			if v == emptyLists || v == presentEmpty {
				continue
			}
			mp := m.Mutable(fd).Map()
			key := scalarValue(fd.MapKey(), v).MapKey()
			if fd.MapValue().Kind() == protoreflect.MessageKind {
				val := mp.NewValue()
				messageValue(val.Message(), depth+1, v)
				mp.Set(key, val)
			} else {
				mp.Set(key, scalarValue(fd.MapValue(), v))
			}
		case fd.IsList():
			if v == emptyLists || v == presentEmpty {
				continue
			}
			l := m.Mutable(fd).List()
			if fd.Kind() == protoreflect.MessageKind {
				el := l.NewElement()
				messageValue(el.Message(), depth+1, v)
				l.Append(el)
			} else {
				l.Append(scalarValue(fd, v))
			}
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			// An empty Any panics aminojson (it slices the empty type URL);
			// no client sends one, so present_empty leaves Anys absent.
			if v == presentEmpty && fd.Message().FullName() == "google.protobuf.Any" {
				continue
			}
			messageValue(m.Mutable(fd).Message(), depth+1, v)
		case v == presentEmpty:
			// scalars stay zero
		default:
			m.Set(fd, scalarValue(fd, v))
		}
	}
}

// presentNonNullable mirrors gogo's zero value: every non-nullable embedded
// message is serialized (empty), nullable ones are absent.
func presentNonNullable(m protoreflect.Message, depth int) {
	if depth > 4 {
		return
	}
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.IsList() || fd.IsMap() || fd.Kind() != protoreflect.MessageKind || !nonNullable(fd) {
			continue
		}
		presentNonNullable(m.Mutable(fd).Message(), depth+1)
	}
}

type record struct {
	TypeURL string          `json:"type_url"`
	Variant string          `json:"variant"`
	Bytes   string          `json:"bytes"`
	Amino   json.RawMessage `json:"amino,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func emit(out *json.Encoder, md protoreflect.MessageDescriptor, v variant) {
	m := dynamicpb.NewMessage(md)
	if v == zero {
		presentNonNullable(m, 0)
	} else {
		messageValue(m, 0, v)
	}
	bz, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		panic(err)
	}
	rec := record{TypeURL: "/" + string(md.FullName()), Variant: variantNames[v], Bytes: hex.EncodeToString(bz)}
	// Render from the bytes, as the chain renders the tx body it decodes.
	dec := dynamicpb.NewMessage(md)
	if err := (proto.UnmarshalOptions{Resolver: types}).Unmarshal(bz, dec); err != nil {
		panic(err)
	}
	if js, err := enc.Marshal(dec); err != nil {
		rec.Error = err.Error()
	} else {
		rec.Amino = js
	}
	if err := out.Encode(rec); err != nil {
		panic(err)
	}
}

// ----------------------------------------------------------------- verify

type signedTx struct {
	Label         string `json:"label"`
	Tx            string `json:"tx"` // hex TxRaw
	ChainID       string `json:"chain_id"`
	AccountNumber uint64 `json:"account_number"`
	Sequence      uint64 `json:"sequence"`
	PubKey        string `json:"pubkey"` // base64 compressed secp256k1
}

func verify() int {
	addrCodec := addresscodec.NewBech32Codec("sprkdrm")
	signingCtx, err := txsigning.NewContext(txsigning.Options{
		FileResolver:          files,
		TypeResolver:          types,
		AddressCodec:          addrCodec,
		ValidatorAddressCodec: addresscodec.NewBech32Codec("sprkdrmvaloper"),
	})
	if err != nil {
		panic(err)
	}
	decoder, err := txdecode.NewDecoder(txdecode.Options{SigningContext: signingCtx})
	if err != nil {
		panic(err)
	}
	handler := aminojson.NewSignModeHandler(aminojson.SignModeHandlerOptions{FileResolver: files, TypeResolver: types})

	failed := 0
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var in signedTx
		if err := json.Unmarshal(scanner.Bytes(), &in); err != nil {
			panic(err)
		}
		if err := verifyOne(decoder, handler, addrCodec, in); err != nil {
			failed++
			fmt.Printf("FAIL %s: %v\n", in.Label, err)
		} else {
			fmt.Printf("ok   %s\n", in.Label)
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func verifyOne(decoder *txdecode.Decoder, handler *aminojson.SignModeHandler, addrCodec address.Codec, in signedTx) error {
	txBytes, err := hex.DecodeString(in.Tx)
	if err != nil {
		return err
	}
	decoded, err := decoder.Decode(txBytes)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	pkBytes, err := base64.StdEncoding.DecodeString(in.PubKey)
	if err != nil {
		return err
	}
	pk := &secp256k1.PubKey{Key: pkBytes}
	addr, err := addrCodec.BytesToString(pk.Address())
	if err != nil {
		return err
	}
	pkAny, err := anypb.New(&secp256k1api.PubKey{Key: pkBytes})
	if err != nil {
		return err
	}
	signBytes, err := handler.GetSignBytes(context.Background(), txsigning.SignerData{
		Address: addr, ChainID: in.ChainID, AccountNumber: in.AccountNumber, Sequence: in.Sequence, PubKey: pkAny,
	}, txsigning.TxData{
		Body: decoded.Tx.Body, AuthInfo: decoded.Tx.AuthInfo,
		BodyBytes: decoded.TxRaw.BodyBytes, AuthInfoBytes: decoded.TxRaw.AuthInfoBytes,
		BodyHasUnknownNonCriticals: decoded.TxBodyHasUnknownNonCriticals,
	})
	if err != nil {
		return fmt.Errorf("sign bytes: %w", err)
	}
	if len(decoded.TxRaw.Signatures) != 1 {
		return fmt.Errorf("want 1 signature, got %d", len(decoded.TxRaw.Signatures))
	}
	if !pk.VerifySignature(signBytes, decoded.TxRaw.Signatures[0]) {
		return fmt.Errorf("signature does not verify; chain sign bytes: %s", signBytes)
	}
	return nil
}

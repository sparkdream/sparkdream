package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/anypb"

	"sparkdream/tools/aminogolden"
)

// appAminoMsgJSON returns the amino JSON the app signs for one message: the
// message bytes are wrapped in an amino-signed tx, decoded by the app's
// TxDecoder and rendered by its sign mode handler, exactly the path
// SigVerificationDecorator takes. It returns the `msgs[0]` entry of the sign
// doc.
func appAminoMsgJSON(app *App, typeURL string, msg []byte) (json.RawMessage, error) {
	field := func(b []byte, num protowire.Number, v []byte) []byte {
		b = protowire.AppendTag(b, num, protowire.BytesType)
		return protowire.AppendBytes(b, v)
	}
	varint := func(b []byte, num protowire.Number, v uint64) []byte {
		b = protowire.AppendTag(b, num, protowire.VarintType)
		return protowire.AppendVarint(b, v)
	}
	pk := secp256k1.GenPrivKey().PubKey()
	signer := sdk.AccAddress(pk.Address()).String()

	body := field(nil, 1, field(field(nil, 1, []byte(typeURL)), 2, msg))
	anyPk := field(field(nil, 1, []byte("/cosmos.crypto.secp256k1.PubKey")), 2, field(nil, 1, pk.Bytes()))
	single := varint(nil, 1, uint64(signingtypes.SignMode_SIGN_MODE_LEGACY_AMINO_JSON))
	signerInfo := field(field(nil, 1, anyPk), 2, field(nil, 1, single))
	coin := field(field(nil, 1, []byte("uspark")), 2, []byte("1"))
	authInfo := field(field(nil, 1, signerInfo), 2, varint(field(nil, 1, coin), 2, 200000))
	txRaw := field(field(field(nil, 1, body), 2, authInfo), 3, make([]byte, 64))

	tx, err := app.TxConfig().TxDecoder()(txRaw)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	adaptable, ok := tx.(authsigning.V2AdaptableTx)
	if !ok {
		return nil, fmt.Errorf("decoded tx %T is not adaptable", tx)
	}
	signBytes, err := app.TxConfig().SignModeHandler().GetSignBytes(
		context.Background(),
		signingv1beta1.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
		txsigning.SignerData{
			Address: signer, ChainID: "phoenix-1", AccountNumber: 1, Sequence: 0,
			PubKey: &anypb.Any{TypeUrl: "/cosmos.crypto.secp256k1.PubKey", Value: field(nil, 1, pk.Bytes())},
		},
		adaptable.GetSigningTxData(),
	)
	if err != nil {
		return nil, fmt.Errorf("sign bytes: %w", err)
	}
	var doc struct {
		Msgs []json.RawMessage `json:"msgs"`
	}
	if err := json.Unmarshal(signBytes, &doc); err != nil {
		return nil, err
	}
	if len(doc.Msgs) != 1 {
		return nil, fmt.Errorf("want 1 msg in the sign doc, got %d", len(doc.Msgs))
	}
	return doc.Msgs[0], nil
}

// TestAminoGoldenMatchesApp ties the amino golden records to the chain. The
// records (tools/aminogolden, exported by cmd/aminogolden) are what
// sparkdreamjs and sparkdream-ui test their amino converters against, so a
// record that differs from what the ante handler signs lets a client pass
// its tests and still produce signatures the chain rejects: that is how
// session keys broke for Ledger users. Every record, in every variant, is
// therefore pushed through the app's own tx decoding and sign mode handler
// and must come out as the same JSON.
//
// A failure after an SDK bump or an aminogolden change means the tool and
// the chain disagree: fix tools/aminogolden, never this test, then
// regenerate the clients' schema and golden files.
func TestAminoGoldenMatchesApp(t *testing.T) {
	h := newMultisigHarness(t)
	records := aminogolden.Records()
	require.Greater(t, len(records), 2000, "golden records missing: registry not loaded?")

	compact := func(js []byte) string {
		var buf bytes.Buffer
		require.NoError(t, json.Compact(&buf, js))
		return buf.String()
	}
	checked, failures := 0, 0
	for _, rec := range records {
		msg, err := hex.DecodeString(rec.Bytes)
		require.NoError(t, err)
		got, err := appAminoMsgJSON(h.app, rec.TypeURL, msg)
		label := rec.TypeURL + " [" + rec.Variant + "]"
		switch {
		case rec.Error != "" && err == nil:
			failures++
			t.Errorf("%s: aminogolden errors (%s) but the app renders %s", label, rec.Error, got)
		case rec.Error != "":
			// both refuse the message
		case err != nil:
			failures++
			t.Errorf("%s: the app refuses what aminogolden renders: %v", label, err)
		case compact(got) != compact(rec.Amino):
			failures++
			t.Errorf("%s:\n  app        %s\n  aminogolden %s", label, compact(got), compact(rec.Amino))
		}
		checked++
		if failures >= 20 {
			t.Fatalf("stopping after %d failures", failures)
		}
	}
	t.Logf("%d golden records match the app", checked-failures)
}

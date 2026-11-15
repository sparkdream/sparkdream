package app

import (
	"context"
	"strings"
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
)

// TestAminoSignBytesCarryNonNullableFields pins a property of amino-JSON
// (Keplr+Ledger) signing that clients must match: the ante handler does not
// render the sign bytes from the message bytes the client sent. Decoding a
// tx, the SDK unpacks each message into its gogoproto Go type and replaces
// the message's bytes with that value's re-encoding, and a field with
// gogoproto.nullable = false is always written there.
//
// The tx below carries a MsgCreateGrant whose session key leaves out `spent`
// and `last_used_at`, as a JS client does (they are the chain's usage
// counters). The sign bytes carry both anyway. A client that signs a document
// without them gets "signature verification failed"; sparkdreamjs and
// cmd/aminogolden mirror this re-encoding. If this test fails after an SDK
// bump, the chain's rendering changed: regenerate the aminogolden schema and
// golden records and re-check the clients before shipping.
func TestAminoSignBytesCarryNonNullableFields(t *testing.T) {
	h := newMultisigHarness(t)

	field := func(b []byte, num protowire.Number, v []byte) []byte {
		b = protowire.AppendTag(b, num, protowire.BytesType)
		return protowire.AppendBytes(b, v)
	}
	varint := func(b []byte, num protowire.Number, v uint64) []byte {
		b = protowire.AppendTag(b, num, protowire.VarintType)
		return protowire.AppendVarint(b, v)
	}

	pk := secp256k1.GenPrivKey().PubKey()
	granter := sdk.AccAddress(pk.Address()).String()
	grantee := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()

	// SessionKeyPayload: allowed_msg_types = 1, spend_limit = 2, max_exec_count = 4.
	// spent = 3 and last_used_at = 6 are left out.
	coin := field(field(nil, 1, []byte("uspark")), 2, []byte("100"))
	sessionKey := field(nil, 1, []byte("/sparkdream.blog.v1.MsgCreatePost"))
	sessionKey = field(sessionKey, 2, coin)
	sessionKey = varint(sessionKey, 4, 10)
	// MsgCreateGrant: granter = 1, grantee = 2, expires_at = 3, session_key = 5.
	msg := field(field(nil, 1, []byte(granter)), 2, []byte(grantee))
	msg = field(msg, 3, varint(nil, 1, 1_900_000_000))
	msg = field(msg, 5, sessionKey)

	anyMsg := field(field(nil, 1, []byte("/sparkdream.session.v1.MsgCreateGrant")), 2, msg)
	body := field(nil, 1, anyMsg)

	anyPk := field(field(nil, 1, []byte("/cosmos.crypto.secp256k1.PubKey")), 2, field(nil, 1, pk.Bytes()))
	single := varint(nil, 1, uint64(signingtypes.SignMode_SIGN_MODE_LEGACY_AMINO_JSON))
	signerInfo := field(field(nil, 1, anyPk), 2, field(nil, 1, single))
	fee := varint(field(nil, 1, coin), 2, 200000)
	authInfo := field(field(nil, 1, signerInfo), 2, fee)

	txRaw := field(field(field(nil, 1, body), 2, authInfo), 3, make([]byte, 64))

	tx, err := h.app.TxConfig().TxDecoder()(txRaw)
	require.NoError(t, err)
	txData := tx.(authsigning.V2AdaptableTx).GetSigningTxData()
	require.Greater(t, len(txData.Body.Messages[0].Value), len(msg),
		"tx decoding no longer re-encodes messages: the amino sign bytes changed for every client")

	signBytes, err := h.app.TxConfig().SignModeHandler().GetSignBytes(
		context.Background(),
		signingv1beta1.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
		txsigning.SignerData{
			Address: granter, ChainID: "phoenix-1", AccountNumber: 1, Sequence: 0,
			PubKey: &anypb.Any{TypeUrl: "/cosmos.crypto.secp256k1.PubKey", Value: field(nil, 1, pk.Bytes())},
		},
		txData,
	)
	require.NoError(t, err)
	got := string(signBytes)
	require.True(t, strings.Contains(got, `"spent":{"amount":"0"}`), got)
	require.True(t, strings.Contains(got, `"last_used_at":"0001-01-01T00:00:00Z"`), got)
}

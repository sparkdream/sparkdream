package types_test

import (
	"encoding/json"
	"testing"

	"cosmossdk.io/math"
	"cosmossdk.io/x/tx/signing/aminojson"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"sparkdream/x/vault/types"
)

// aminoJSON renders msg the way the chain's aminojson sign mode does for
// Keplr+Ledger (amino-JSON is the only mode Ledger signs).
func aminoJSON(t *testing.T, msg gogoproto.Message) map[string]any {
	t.Helper()
	files, err := gogoproto.MergedRegistry()
	require.NoError(t, err)
	desc, err := files.FindDescriptorByName(protoreflect.FullName(gogoproto.MessageName(msg)))
	require.NoError(t, err)
	bz, err := gogoproto.Marshal(msg)
	require.NoError(t, err)
	dyn := dynamicpb.NewMessage(desc.(protoreflect.MessageDescriptor))
	require.NoError(t, proto.Unmarshal(bz, dyn))

	enc := aminojson.NewEncoder(aminojson.EncoderOptions{FileResolver: files, TypeResolver: dynamicpb.NewTypes(files)})
	out, err := enc.Marshal(dyn)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out, &doc))
	return doc
}

// TestMsgProposeAminoJSON pins how a proposal with an inner message renders
// under amino-JSON: each inner Any becomes {"type": <inner amino name>,
// "value": ...}, uint64s are strings, and false/empty fields are omitted.
// Client converters (sparkdreamjs) must produce exactly this.
func TestMsgProposeAminoJSON(t *testing.T) {
	vault := sdk.AccAddress([]byte("vault_______________")).String()
	signer := sdk.AccAddress([]byte("signer______________")).String()
	inner, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: vault, ToAddress: signer,
		Amount: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5))),
	})
	require.NoError(t, err)

	doc := aminoJSON(t, &types.MsgPropose{Signer: signer, VaultId: 7, Messages: []*codectypes.Any{inner}})
	require.Equal(t, "sparkdream/x/vault/MsgPropose", doc["type"])
	value := doc["value"].(map[string]any)
	require.Equal(t, "7", value["vault_id"])
	require.NotContains(t, value, "execute", "false bool is omitted")
	require.NotContains(t, value, "memo", "empty string is omitted")
	msgs := value["messages"].([]any)
	require.Len(t, msgs, 1)
	send := msgs[0].(map[string]any)
	require.Equal(t, "cosmos-sdk/MsgSend", send["type"])
	require.Equal(t, vault, send["value"].(map[string]any)["from_address"])

	// No inner messages: the repeated field is omitted, not [].
	doc = aminoJSON(t, &types.MsgPropose{Signer: signer, VaultId: 7, Execute: true})
	value = doc["value"].(map[string]any)
	require.NotContains(t, value, "messages")
	require.Equal(t, true, value["execute"])
}

// TestMsgApproveAminoJSON pins the bytes field: messages_hash renders as
// base64.
func TestMsgApproveAminoJSON(t *testing.T) {
	signer := sdk.AccAddress([]byte("signer______________")).String()
	doc := aminoJSON(t, &types.MsgApprove{Signer: signer, ProposalId: 3, MessagesHash: []byte{0xde, 0xad, 0xbe, 0xef}})
	require.Equal(t, "sparkdream/x/vault/MsgApprove", doc["type"])
	value := doc["value"].(map[string]any)
	require.Equal(t, "3", value["proposal_id"])
	require.Equal(t, "3q2+7w==", value["messages_hash"])
}

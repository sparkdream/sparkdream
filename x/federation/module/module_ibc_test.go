package federation_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	federation "sparkdream/x/federation/module"
	"sparkdream/x/federation/types"
)

// These tests drive the ICS-26 callbacks with ASYMMETRIC channel ids — this
// chain's end of the federation channel is channel-7, the peer's end is
// channel-42 — which is the normal case between two independent chains and
// the case the old source-channel lookup got wrong (FED-01).

const (
	ourEnd   = "channel-7"  // Peer.ibc_channel_id: this chain's end
	theirEnd = "channel-42" // the peer chain's own id for the same channel
	peerID   = "phoenix-1"
)

type ibcFixture struct {
	ctx sdk.Context
	k   keeper.Keeper
	im  federation.IBCModule
	enc moduletestutil.TestEncodingConfig
}

type nopAuth struct{ ac address.Codec }

func (a nopAuth) AddressCodec() address.Codec { return a.ac }
func (a nopAuth) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}
func (nopAuth) GetModuleAddress(name string) sdk.AccAddress { return authtypes.NewModuleAddress(name) }

type nopBank struct{}

func (nopBank) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (nopBank) GetBalance(_ context.Context, _ sdk.AccAddress, d string) sdk.Coin {
	return sdk.NewInt64Coin(d, 0)
}
func (nopBank) SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (nopBank) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}
func (nopBank) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (nopBank) BurnCoins(context.Context, string, sdk.Coins) error   { return nil }
func (nopBank) SetDenomMetaData(context.Context, banktypes.Metadata) {}
func (nopBank) GetDenomMetaData(context.Context, string) (banktypes.Metadata, bool) {
	return banktypes.Metadata{}, false
}

func newIBCFixture(t *testing.T) ibcFixture {
	t.Helper()
	enc := moduletestutil.MakeTestEncodingConfig(federation.AppModule{})
	ac := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	key := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_ibc")).Ctx
	k := keeper.NewKeeper(runtime.NewKVStoreService(key), enc.Codec, ac,
		authtypes.NewModuleAddress(govtypes.ModuleName), nopAuth{ac}, nopBank{}, nil)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	require.NoError(t, k.Port.Set(ctx, types.PortID))
	require.NoError(t, k.Peers.Set(ctx, peerID, types.Peer{
		Id: peerID, Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		Status: types.PeerStatus_PEER_STATUS_ACTIVE, IbcChannelId: ourEnd,
	}))
	require.NoError(t, k.PeerPolicies.Set(ctx, peerID, types.PeerPolicy{
		PeerId: peerID, InboundContentTypes: []string{"blog_post"},
		AcceptReputationAttestations: true,
	}))
	return ibcFixture{ctx: ctx, k: k, im: federation.NewIBCModule(enc.Codec, k), enc: enc}
}

func contentPacketData(t *testing.T, seed byte) []byte {
	t.Helper()
	h := make([]byte, 32)
	for i := range h {
		h[i] = seed
	}
	pd := types.FederationPacketData{Packet: &types.FederationPacketData_Content{Content: &types.ContentPacket{
		ContentType: "blog_post", RemoteContentId: "post", Creator: "phoenix1author",
		Title: "t", Body: "b", ContentHash: h, License: "CC0-1.0",
	}}}
	bz, err := pd.Marshal()
	require.NoError(t, err)
	return bz
}

func packet(data []byte, srcChannel, dstPort, dstChannel string) channeltypes.Packet {
	return channeltypes.Packet{
		Sequence: 1, SourcePort: types.PortID, SourceChannel: srcChannel,
		DestinationPort: dstPort, DestinationChannel: dstChannel, Data: data,
	}
}

func storedContentPeers(t *testing.T, f ibcFixture) []string {
	t.Helper()
	var peers []string
	require.NoError(t, f.k.Content.Walk(f.ctx, nil, func(_ uint64, c types.FederatedContent) (bool, error) {
		peers = append(peers, c.PeerId)
		return false, nil
	}))
	return peers
}

// The honest peer's packet arrives on OUR end (channel-7) carrying THEIR end
// (channel-42) as source: it must be accepted and credited to the peer.
func TestOnRecvPacket_AsymmetricChannelsHonestPeerAccepted(t *testing.T) {
	f := newIBCFixture(t)
	ack := f.im.OnRecvPacket(f.ctx, types.Version, packet(contentPacketData(t, 1), theirEnd, types.PortID, ourEnd), nil)
	require.True(t, ack.Success(), string(ack.Acknowledgement()))
	require.Equal(t, []string{peerID}, storedContentPeers(t, f))
}

// A spoofing chain opens a channel to our federation port and names ITS end
// channel-7, the registered peer's local id. Its packets arrive on our
// channel-42 with SourceChannel = channel-7. Matching the source channel
// would attribute them to the peer; they must be refused.
func TestOnRecvPacket_SpoofedSourceChannelRejected(t *testing.T) {
	f := newIBCFixture(t)
	ack := f.im.OnRecvPacket(f.ctx, types.Version, packet(contentPacketData(t, 2), ourEnd, types.PortID, theirEnd), nil)
	require.False(t, ack.Success())
	require.Empty(t, storedContentPeers(t, f))

	// The same spoof aimed at the identity-confirmation path cannot verify
	// someone's link either.
	victim := sdk.AccAddress([]byte("victim______________")).String()
	require.NoError(t, f.k.IdentityLinks.Set(f.ctx, collections.Join(victim, peerID), types.IdentityLink{
		LocalAddress: victim, PeerId: peerID, RemoteIdentity: "phoenix1celebrity",
		Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, Challenge: []byte("public-challenge"),
	}))
	confirm := types.FederationPacketData{Packet: &types.FederationPacketData_IdentityConfirmation{
		IdentityConfirmation: &types.IdentityVerificationConfirmPacket{
			ClaimedAddress: "phoenix1celebrity", ClaimantAddress: victim, Challenge: []byte("public-challenge"), Confirmed: true,
		},
	}}
	bz, _ := confirm.Marshal()
	ack = f.im.OnRecvPacket(f.ctx, types.Version, packet(bz, ourEnd, types.PortID, theirEnd), nil)
	require.False(t, ack.Success())
	link, _ := f.k.IdentityLinks.Get(f.ctx, collections.Join(victim, peerID))
	require.Equal(t, types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED, link.Status)
}

// The destination port must be the federation port.
func TestOnRecvPacket_WrongPortRejected(t *testing.T) {
	f := newIBCFixture(t)
	ack := f.im.OnRecvPacket(f.ctx, types.Version, packet(contentPacketData(t, 3), theirEnd, "transfer", ourEnd), nil)
	require.False(t, ack.Success())
	require.Empty(t, storedContentPeers(t, f))
}

// A suspended peer's packets get an error acknowledgement (FED-02).
func TestOnRecvPacket_SuspendedPeerRejected(t *testing.T) {
	f := newIBCFixture(t)
	peer, _ := f.k.Peers.Get(f.ctx, peerID)
	peer.Status = types.PeerStatus_PEER_STATUS_SUSPENDED
	require.NoError(t, f.k.Peers.Set(f.ctx, peerID, peer))
	ack := f.im.OnRecvPacket(f.ctx, types.Version, packet(contentPacketData(t, 4), theirEnd, types.PortID, ourEnd), nil)
	require.False(t, ack.Success())
}

// Acks and timeouts concern packets WE sent, so the packet's source channel
// is our end and identifies the peer: a reputation answer on channel-7 is
// credited to the peer.
func TestOnAcknowledgementPacket_UsesOurSourceChannel(t *testing.T) {
	f := newIBCFixture(t)
	requester := sdk.AccAddress([]byte("requester___________")).String()
	require.NoError(t, f.k.IdentityLinks.Set(f.ctx, collections.Join(requester, peerID), types.IdentityLink{
		LocalAddress: requester, PeerId: peerID, RemoteIdentity: "phoenix1me",
		Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED,
	}))
	q := types.FederationPacketData{Packet: &types.FederationPacketData_ReputationQuery{
		ReputationQuery: &types.ReputationQueryPacket{Requester: requester, QueriedAddress: "phoenix1me"},
	}}
	data, _ := q.Marshal()
	resp, _ := (&types.ReputationResponseData{Address: "phoenix1me", TrustLevel: 2, IsActive: true}).Marshal()
	ackBz := channeltypes.NewResultAcknowledgement(resp).Acknowledgement()

	sent := channeltypes.Packet{Sequence: 9, SourcePort: types.PortID, SourceChannel: ourEnd,
		DestinationPort: types.PortID, DestinationChannel: theirEnd, Data: data}
	require.NoError(t, f.im.OnAcknowledgementPacket(f.ctx, types.Version, sent, ackBz, nil))
	att, err := f.k.RepAttestations.Get(f.ctx, collections.Join(requester, peerID))
	require.NoError(t, err)
	require.Equal(t, peerID, att.PeerId)

	// FED-10: a garbage acknowledgement is dropped, not returned as an
	// error that would wedge the packet commitment forever.
	require.NoError(t, f.im.OnAcknowledgementPacket(f.ctx, types.Version, sent, []byte("not json"), nil))
}

// Handshake: UNORDERED only, federation port on both ends (spec §8.1-8.2).
func TestChannelHandshakeValidation(t *testing.T) {
	f := newIBCFixture(t)
	cp := channeltypes.NewCounterparty(types.PortID, "")
	_, err := f.im.OnChanOpenInit(f.ctx, channeltypes.UNORDERED, nil, types.PortID, ourEnd, cp, types.Version)
	require.NoError(t, err)
	_, err = f.im.OnChanOpenInit(f.ctx, channeltypes.ORDERED, nil, types.PortID, ourEnd, cp, types.Version)
	require.Error(t, err)
	_, err = f.im.OnChanOpenTry(f.ctx, channeltypes.UNORDERED, nil, types.PortID, ourEnd,
		channeltypes.NewCounterparty("transfer", theirEnd), types.Version)
	require.Error(t, err)
	_, err = f.im.OnChanOpenTry(f.ctx, channeltypes.UNORDERED, nil, types.PortID, ourEnd,
		channeltypes.NewCounterparty(types.PortID, theirEnd), types.Version)
	require.NoError(t, err)
}

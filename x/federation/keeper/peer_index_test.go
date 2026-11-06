package keeper_test

import (
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// FED-101: display_name and metadata are capped on both write paths.
func TestPeerInfoSizeCapped(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	big := strings.Repeat("x", types.MaxPeerMetadataSize+1)
	reg := &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "bloat-peer", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB,
		Metadata: big,
	}
	require.Error(t, reg.ValidateBasic())
	_, err := ms.RegisterPeer(f.ctx, reg)
	require.ErrorContains(t, err, "metadata")

	reg.Metadata = ""
	reg.DisplayName = strings.Repeat("n", types.MaxPeerDisplayNameSize+1)
	_, err = ms.RegisterPeer(f.ctx, reg)
	require.ErrorContains(t, err, "display_name")

	registerTestPeer(t, f, ms, "info-peer")
	upd := &types.MsgUpdatePeerInfo{Authority: f.authority, PeerId: "info-peer", Metadata: big}
	require.Error(t, upd.ValidateBasic())
	_, err = ms.UpdatePeerInfo(f.ctx, upd)
	require.ErrorContains(t, err, "metadata")

	upd.Metadata = strings.Repeat("x", types.MaxPeerMetadataSize)
	require.NoError(t, upd.ValidateBasic())
	_, err = ms.UpdatePeerInfo(f.ctx, upd)
	require.NoError(t, err)

	gs := types.DefaultGenesis()
	gs.Peers = []types.Peer{{Id: "bloat-peer", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB, Metadata: big}}
	require.ErrorContains(t, gs.Validate(), "metadata")
}

// FED-101: channel lookups go through the derived index and follow every
// status change; their gas does not grow with the registry.
func TestPeerChannelIndex(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-7", IbcTransferChannelId: "channel-8", ExpectedChainId: testPeerChainID,
	})
	require.NoError(t, err)

	// PENDING: bound, but not an active relay channel.
	id, err := f.keeper.FindPeerByChannelForTest(f.ctx, "channel-7")
	require.NoError(t, err)
	require.Equal(t, "phoenix", id)
	require.False(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-7"))

	// a second peer cannot take the channel
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "aurora", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-7", ExpectedChainId: testPeerChainID,
	})
	require.ErrorContains(t, err, "already bound")

	activateTestPeer(t, f, ms, "phoenix")
	require.True(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-7"))
	require.True(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-8"))
	require.False(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-9"))

	// many max-size peers do not change the lookup cost
	measure := func(ch string) storetypes.Gas {
		ctx := f.sdkCtx().WithGasMeter(storetypes.NewInfiniteGasMeter())
		f.keeper.IsActivePeerChannel(ctx, ch)
		_, _ = f.keeper.FindPeerByChannelForTest(ctx, ch)
		return ctx.GasMeter().GasConsumed()
	}
	before := measure("channel-99")
	for i := 0; i < 20; i++ {
		_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
			Authority: f.authority, PeerId: "zenith-" + strings.Repeat("a", i+1), Type: types.PeerType_PEER_TYPE_ACTIVITYPUB,
			DisplayName: strings.Repeat("n", types.MaxPeerDisplayNameSize),
			Metadata:    strings.Repeat("m", types.MaxPeerMetadataSize),
		})
		require.NoError(t, err)
	}
	require.Equal(t, before, measure("channel-99"))

	_, err = ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{Authority: f.authority, PeerId: "phoenix", Reason: "test"})
	require.NoError(t, err)
	require.False(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-7"))
	id, err = f.keeper.FindPeerByChannelForTest(f.ctx, "channel-7")
	require.NoError(t, err)
	require.Equal(t, "phoenix", id)

	_, err = ms.RemovePeer(f.ctx, &types.MsgRemovePeer{Authority: f.authority, PeerId: "phoenix", Reason: "test"})
	require.NoError(t, err)
	_, err = f.keeper.FindPeerByChannelForTest(f.ctx, "channel-7")
	require.Error(t, err)

	// the freed channel can be bound again
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "aurora", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-7", ExpectedChainId: testPeerChainID,
	})
	require.NoError(t, err)
}

// FED-101: InitGenesis rebuilds the channel indexes from the peer records.
func TestPeerChannelIndexRebuiltAtGenesis(t *testing.T) {
	gs := types.DefaultGenesis()
	gs.Peers = []types.Peer{
		{Id: "phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM, Status: types.PeerStatus_PEER_STATUS_ACTIVE,
			IbcChannelId: "channel-1", IbcTransferChannelId: "channel-2", ExpectedChainId: "phoenix-1"},
		{Id: "aurora", Type: types.PeerType_PEER_TYPE_SPARK_DREAM, Status: types.PeerStatus_PEER_STATUS_REMOVED,
			IbcChannelId: "channel-3", ExpectedChainId: "aurora-1"},
	}
	require.NoError(t, gs.Validate())

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))
	id, err := f.keeper.FindPeerByChannelForTest(f.ctx, "channel-1")
	require.NoError(t, err)
	require.Equal(t, "phoenix", id)
	require.True(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-2"))
	_, err = f.keeper.FindPeerByChannelForTest(f.ctx, "channel-3")
	require.Error(t, err)

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, got.Peers, 2)

	// two live peers on one channel are refused
	gs.Peers[1].Status = types.PeerStatus_PEER_STATUS_PENDING
	gs.Peers[1].IbcChannelId = "channel-1"
	require.ErrorContains(t, gs.Validate(), "share ibc channel")
}

// FED-106: a Spark Dream peer is activated only once its federation channel
// exists on the federation port, is OPEN, and its light client names the
// peer's expected_chain_id.
func TestResumePeerRequiresOpenChannel(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-12", ExpectedChainId: "phoenix-1",
	})
	require.NoError(t, err)

	resume := func() error {
		_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{Authority: f.authority, PeerId: "phoenix"})
		return err
	}
	require.ErrorContains(t, resume(), "does not exist")

	openTestChannelTo(t, f, "channel-12", channeltypes.INIT, "phoenix-1")
	require.ErrorContains(t, resume(), "not OPEN")

	// OPEN, but the client behind it is another chain: refused.
	openTestChannelTo(t, f, "channel-12", channeltypes.OPEN, "attacker-1")
	err = resume()
	require.ErrorIs(t, err, types.ErrPeerChainIdMismatch)
	require.ErrorContains(t, err, `leads to chain "attacker-1"`)
	peer, getErr := f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, getErr)
	require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status)

	// OPEN, but the client cannot be resolved: refused as well.
	openTestChannelTo(t, f, "channel-12", channeltypes.OPEN, "phoenix-1")
	dropTestClient(t, f, "channel-12")
	require.ErrorIs(t, resume(), types.ErrPeerChainIdMismatch)

	// OPEN with the expected chain behind it: activated.
	openTestChannelTo(t, f, "channel-12", channeltypes.OPEN, "phoenix-1")
	require.NoError(t, resume())
	require.True(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-12"))
	peer, getErr = f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, getErr)
	require.Equal(t, "phoenix-1", peer.ExpectedChainId)
}

// FED-106: a channel with no connection hop cannot name a chain and is not
// trusted.
func TestResumePeerRefusesChannelWithoutConnection(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-3", ExpectedChainId: "phoenix-1",
	})
	require.NoError(t, err)
	ibcK := f.keeper.IBCKeeperForTest()
	ibcK.ChannelKeeper.SetChannel(sdk.UnwrapSDKContext(f.ctx), types.PortID, "channel-3", channeltypes.NewChannel(
		channeltypes.OPEN, channeltypes.UNORDERED, channeltypes.NewCounterparty(types.PortID, "channel-0"),
		nil, types.Version))
	_, err = ms.ResumePeer(f.ctx, &types.MsgResumePeer{Authority: f.authority, PeerId: "phoenix"})
	require.ErrorIs(t, err, types.ErrPeerChainIdMismatch)
	require.ErrorContains(t, err, "no connection hops")
}

// FED-201: the ICS-20 transfer channel a Spark Dream peer names is verified
// at activation like the federation channel (OPEN on the transfer port,
// light client naming expected_chain_id), and the voucher metadata keyed on
// it is written only then.
func TestResumePeerVerifiesTransferChannel(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	identity := testPeerIdentity("Phoenix", "PHX", "uspk.phoenix", "PSPK", "udrm.phoenix", "PDRM")
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix", Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId: "channel-4", IbcTransferChannelId: "channel-5", ExpectedChainId: "phoenix-1",
		PeerIdentity: identity,
	})
	require.NoError(t, err)
	require.Empty(t, f.bankKeeper.metadata, "no voucher metadata before the transfer channel is verified")

	resume := func() error {
		_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{Authority: f.authority, PeerId: "phoenix"})
		return err
	}
	openTestChannelTo(t, f, "channel-4", channeltypes.OPEN, "phoenix-1")

	// federation channel fine, transfer channel missing
	err = resume()
	require.ErrorContains(t, err, "channel-5 does not exist on port transfer")

	// a transfer channel on the federation port does not count
	openTestChannelTo(t, f, "channel-5", channeltypes.OPEN, "phoenix-1")
	require.ErrorContains(t, resume(), "channel-5 does not exist on port transfer")

	// transfer channel leads to another chain: refused, still PENDING
	openTestChannelOn(t, f, ibctransfertypes.PortID, "channel-5", channeltypes.OPEN, "attacker-1")
	err = resume()
	require.ErrorIs(t, err, types.ErrPeerChainIdMismatch)
	require.ErrorContains(t, err, "on port transfer")
	peer, getErr := f.keeper.Peers.Get(f.ctx, "phoenix")
	require.NoError(t, getErr)
	require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status)
	require.Empty(t, f.bankKeeper.metadata)

	// both channels verified: activated, metadata written once
	openTestChannelOn(t, f, ibctransfertypes.PortID, "channel-5", channeltypes.OPEN, "phoenix-1")
	require.NoError(t, resume())
	require.True(t, f.keeper.IsActivePeerChannel(f.ctx, "channel-5"))
	require.Len(t, f.bankKeeper.metadata, 1)

	// suspend + resume is idempotent for the metadata
	_, err = ms.SuspendPeer(f.ctx, &types.MsgSuspendPeer{Authority: f.authority, PeerId: "phoenix", Reason: "test"})
	require.NoError(t, err)
	require.NoError(t, resume())
	require.Len(t, f.bankKeeper.metadata, 1)
}

package keeper_test

import (
	"testing"

	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	identitytypes "sparkdream/x/identity/types"
)

func TestRegisterPeer(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	tests := []struct {
		name      string
		msg       *types.MsgRegisterPeer
		expErr    bool
		expErrMsg string
	}{
		{
			name: "valid spark dream peer",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "sparkdream-2", DisplayName: "Chain 2",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-0",
			},
		},
		{
			name: "valid activitypub peer",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "mastodon.social", DisplayName: "Mastodon",
				Type: types.PeerType_PEER_TYPE_ACTIVITYPUB,
			},
		},
		{
			name: "valid atproto peer",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "bsky.social", DisplayName: "Bluesky",
				Type: types.PeerType_PEER_TYPE_ATPROTO,
			},
		},
		{
			name: "invalid peer id too short",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "ab", DisplayName: "Bad",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
			},
			expErr: true, expErrMsg: "peer ID",
		},
		{
			name: "invalid peer id uppercase",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "BadPeer", DisplayName: "Bad",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
			},
			expErr: true, expErrMsg: "peer ID",
		},
		{
			name: "unspecified type rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "valid-peer", DisplayName: "Valid",
				Type: types.PeerType_PEER_TYPE_UNSPECIFIED,
			},
			expErr: true, expErrMsg: "peer type must be specified",
		},
		{
			name: "duplicate peer rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "sparkdream-2", DisplayName: "Dup",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM,
			},
			expErr: true, expErrMsg: "already exists",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ms.RegisterPeer(f.ctx, tc.msg)
			if tc.expErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErrMsg)
			} else {
				require.NoError(t, err)
				peer, err := f.keeper.Peers.Get(f.ctx, tc.msg.PeerId)
				require.NoError(t, err)
				require.Equal(t, tc.msg.PeerId, peer.Id)
				require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status)
				_, err = f.keeper.PeerPolicies.Get(f.ctx, tc.msg.PeerId)
				require.NoError(t, err)
			}
		})
	}
}

// Voucher metadata must follow the ICS-20 transfer channel, not the
// federation channel: the two are opened separately and rarely share a
// number, and a trace built on the wrong one names a denom that never
// arrives.
func TestRegisterPeerVoucherMetadataUsesTransferChannel(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	identity := &identitytypes.ChainIdentity{
		BondDenom:           "uspk.phoenix",
		BondDisplaySymbol:   "PSPK",
		BondDisplayName:     "Phoenix Spark",
		BondDisplayDecimals: 6,
	}
	voucher := func(channel string) string {
		return ibctransfertypes.Denom{
			Base:  identity.BondDenom,
			Trace: []ibctransfertypes.Hop{{PortId: ibctransfertypes.PortID, ChannelId: channel}},
		}.IBCDenom()
	}

	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix-1", DisplayName: "Phoenix",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-0",
		IbcTransferChannelId: "channel-1", PeerIdentity: identity,
	})
	require.NoError(t, err)

	md, ok := f.bankKeeper.GetDenomMetaData(f.ctx, voucher("channel-1"))
	require.True(t, ok, "metadata registered under the transfer channel's voucher")
	require.Equal(t, "PSPK.ibc", md.Symbol)
	_, ok = f.bankKeeper.GetDenomMetaData(f.ctx, voucher("channel-0"))
	require.False(t, ok, "no metadata under the federation channel")

	peer, err := f.keeper.Peers.Get(f.ctx, "phoenix-1")
	require.NoError(t, err)
	require.Equal(t, "channel-1", peer.IbcTransferChannelId)

	// No transfer channel: registration succeeds, metadata is skipped.
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "aurora-1", DisplayName: "Aurora",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-2",
		PeerIdentity: &identitytypes.ChainIdentity{BondDenom: "uspk.aurora", BondDisplaySymbol: "ASPK"},
	})
	require.NoError(t, err)
	require.Len(t, f.bankKeeper.metadata, 1)

	// Malformed transfer channel id is rejected.
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "zenith-1", DisplayName: "Zenith",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcTransferChannelId: "not-a-channel",
	})
	require.ErrorContains(t, err, "invalid ibc transfer channel id")
}

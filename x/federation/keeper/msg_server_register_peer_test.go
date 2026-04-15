package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
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

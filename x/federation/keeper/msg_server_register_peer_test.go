package keeper_test

import (
	"strings"
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
				ExpectedChainId: "sparkdream-2",
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
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM, ExpectedChainId: "sparkdream-2",
			},
			expErr: true, expErrMsg: "already exists",
		},
		{
			name: "spark dream peer without expected_chain_id rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "sparkdream-3", DisplayName: "Chain 3",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-1",
			},
			expErr: true, expErrMsg: "expected_chain_id is required",
		},
		{
			name: "spark dream peer with whitespace in expected_chain_id rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "sparkdream-3", DisplayName: "Chain 3",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-1",
				ExpectedChainId: "spark dream-3",
			},
			expErr: true, expErrMsg: "whitespace",
		},
		{
			name: "spark dream peer with overlong expected_chain_id rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "sparkdream-3", DisplayName: "Chain 3",
				Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-1",
				ExpectedChainId: strings.Repeat("c", types.MaxPeerChainIdSize+1),
			},
			expErr: true, expErrMsg: "max 50",
		},
		{
			name: "activitypub peer with expected_chain_id rejected",
			msg: &types.MsgRegisterPeer{
				Authority: f.authority, PeerId: "pleroma.example", DisplayName: "Pleroma",
				Type: types.PeerType_PEER_TYPE_ACTIVITYPUB, ExpectedChainId: "pleroma-1",
			},
			expErr: true, expErrMsg: "only for Spark Dream peers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ms.RegisterPeer(f.ctx, tc.msg)
			if tc.expErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErrMsg)
			} else {
				require.NoError(t, tc.msg.ValidateBasic())
				require.NoError(t, err)
				peer, err := f.keeper.Peers.Get(f.ctx, tc.msg.PeerId)
				require.NoError(t, err)
				require.Equal(t, tc.msg.PeerId, peer.Id)
				require.Equal(t, tc.msg.ExpectedChainId, peer.ExpectedChainId)
				require.Equal(t, types.PeerStatus_PEER_STATUS_PENDING, peer.Status)
				_, err = f.keeper.PeerPolicies.Get(f.ctx, tc.msg.PeerId)
				require.NoError(t, err)
			}
		})
	}
}

// FED-106: ValidateBasic enforces the same expected_chain_id rules as the
// handler, so a malformed registration never reaches a proposal.
func TestRegisterPeerValidateBasicExpectedChainID(t *testing.T) {
	ibc := &types.MsgRegisterPeer{PeerId: "phoenix-1", Type: types.PeerType_PEER_TYPE_SPARK_DREAM}
	require.ErrorContains(t, ibc.ValidateBasic(), "expected_chain_id is required")
	ibc.ExpectedChainId = "phoenix\t1"
	require.ErrorContains(t, ibc.ValidateBasic(), "whitespace")
	ibc.ExpectedChainId = "phoenix-1"
	require.NoError(t, ibc.ValidateBasic())

	ap := &types.MsgRegisterPeer{PeerId: "phoenix.example", Type: types.PeerType_PEER_TYPE_ACTIVITYPUB}
	require.NoError(t, ap.ValidateBasic())
	ap.ExpectedChainId = "phoenix-1"
	require.ErrorContains(t, ap.ValidateBasic(), "only for Spark Dream peers")
}

// Voucher metadata must follow the ICS-20 transfer channel, not the
// federation channel: the two are opened separately and rarely share a
// number, and a trace built on the wrong one names a denom that never
// arrives. It is written at activation, once the transfer channel has been
// verified, never at 1-of-N registration (FED-201).
func TestRegisterPeerVoucherMetadataUsesTransferChannel(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	identity := testPeerIdentity("Phoenix", "PHX", "uspk.phoenix", "PSPK", "udrm.phoenix", "PDRM")
	voucher := func(channel string) string {
		return ibctransfertypes.Denom{
			Base:  identity.BondDenom,
			Trace: []ibctransfertypes.Hop{{PortId: ibctransfertypes.PortID, ChannelId: channel}},
		}.IBCDenom()
	}

	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "phoenix-1", DisplayName: "Phoenix",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-0",
		IbcTransferChannelId: "channel-1", PeerIdentity: identity, ExpectedChainId: "phoenix-1",
	})
	require.NoError(t, err)
	require.Empty(t, f.bankKeeper.metadata, "registration verifies nothing and writes no metadata")

	activateTestPeer(t, f, ms, "phoenix-1")
	md, ok := f.bankKeeper.GetDenomMetaData(f.ctx, voucher("channel-1"))
	require.True(t, ok, "metadata registered under the transfer channel's voucher at activation")
	require.Equal(t, "PSPK.ibc", md.Symbol)
	_, ok = f.bankKeeper.GetDenomMetaData(f.ctx, voucher("channel-0"))
	require.False(t, ok, "no metadata under the federation channel")

	peer, err := f.keeper.Peers.Get(f.ctx, "phoenix-1")
	require.NoError(t, err)
	require.Equal(t, "channel-1", peer.IbcTransferChannelId)

	// No transfer channel: activation succeeds, metadata is skipped.
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "aurora-1", DisplayName: "Aurora",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-2", ExpectedChainId: "aurora-1",
		PeerIdentity: testPeerIdentity("Aurora", "AUR", "uspk.aurora", "ASPK", "udrm.aurora", "ADRM"),
	})
	require.NoError(t, err)
	activateTestPeer(t, f, ms, "aurora-1")
	require.Len(t, f.bankKeeper.metadata, 1)

	// Malformed transfer channel id is rejected.
	_, err = ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "zenith-1", DisplayName: "Zenith",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcTransferChannelId: "not-a-channel", ExpectedChainId: "zenith-1",
	})
	require.ErrorContains(t, err, "invalid ibc transfer channel id")
}

// testPeerIdentity builds a ChainIdentity that passes Validate.
func testPeerIdentity(name, ticker, bondDenom, bondSym, dreamDenom, dreamSym string) *identitytypes.ChainIdentity {
	return &identitytypes.ChainIdentity{
		ChainHumanName:       name,
		ChainTickerPrefix:    ticker,
		BondDenom:            bondDenom,
		BondDisplaySymbol:    bondSym,
		BondDisplayName:      name + " Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           dreamDenom,
		DreamDisplaySymbol:   dreamSym,
		DreamDisplayName:     name + " Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

// A supplied peer_identity must be well-formed; a partial record is
// rejected instead of stored.
func TestRegisterPeerRejectsMalformedIdentity(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority: f.authority, PeerId: "nova-1", DisplayName: "Nova",
		Type: types.PeerType_PEER_TYPE_SPARK_DREAM, IbcChannelId: "channel-7", ExpectedChainId: "nova-1",
		PeerIdentity: &identitytypes.ChainIdentity{BondDenom: "uspk.nova", BondDisplaySymbol: "NSPK"},
	})
	require.ErrorContains(t, err, "invalid peer_identity")
	_, err = f.keeper.Peers.Get(f.ctx, "nova-1")
	require.Error(t, err, "malformed registration must not be stored")
}

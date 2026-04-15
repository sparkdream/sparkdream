package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgRegisterPeer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		peerID := randomPeerID(r)

		// Check if peer already exists
		_, err := k.Peers.Get(ctx, peerID)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterPeer{}), "peer already exists"), nil, nil
		}

		peer := types.Peer{
			Id:           peerID,
			DisplayName:  fmt.Sprintf("Sim Peer %s", peerID[:8]),
			Type:         randomPeerType(r),
			Status:       types.PeerStatus_PEER_STATUS_ACTIVE,
			IbcChannelId: fmt.Sprintf("channel-%d", r.Intn(100)),
			RegisteredAt: ctx.BlockTime().Unix(),
			LastActivity: ctx.BlockTime().Unix(),
			RegisteredBy: simAccount.Address.String(),
			Metadata:     "simulation registered peer",
		}

		if err := k.Peers.Set(ctx, peerID, peer); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterPeer{}), "failed to set peer"), nil, nil
		}

		// Create default policy
		policy := types.PeerPolicy{
			PeerId:                       peerID,
			OutboundContentTypes:         types.DefaultKnownContentTypes,
			InboundContentTypes:          types.DefaultKnownContentTypes,
			MinOutboundTrustLevel:        1,
			InboundRateLimitPerEpoch:     uint64(r.Intn(200) + 50),
			OutboundRateLimitPerEpoch:    uint64(r.Intn(200) + 50),
			AllowReputationQueries:       r.Intn(2) == 1,
			AcceptReputationAttestations: r.Intn(2) == 1,
			MaxTrustCredit:               1,
		}
		if err := k.PeerPolicies.Set(ctx, peerID, policy); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterPeer{}), "failed to set policy"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterPeer{}), "ok (direct keeper call)"), nil, nil
	}
}

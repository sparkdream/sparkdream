package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgUpdatePeerPolicy(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		peer, err := getOrCreateActivePeer(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePeerPolicy{}), "failed to get/create active peer"), nil, nil
		}

		policy := types.PeerPolicy{
			PeerId:                       peer.Id,
			OutboundContentTypes:         randomContentTypeSubset(r),
			InboundContentTypes:          randomContentTypeSubset(r),
			MinOutboundTrustLevel:        uint32(r.Intn(3) + 1),
			InboundRateLimitPerEpoch:     uint64(r.Intn(500) + 10),
			OutboundRateLimitPerEpoch:    uint64(r.Intn(500) + 10),
			AllowReputationQueries:       r.Intn(2) == 1,
			AcceptReputationAttestations: r.Intn(2) == 1,
			MaxTrustCredit:               uint32(r.Intn(3) + 1),
			RequireReview:                r.Intn(2) == 1,
		}

		if err := k.PeerPolicies.Set(ctx, peer.Id, policy); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePeerPolicy{}), "failed to set policy"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePeerPolicy{}), "ok (direct keeper call)"), nil, nil
	}
}

// randomContentTypeSubset returns a random non-empty subset of known content types.
func randomContentTypeSubset(r *rand.Rand) []string {
	all := types.DefaultKnownContentTypes
	n := r.Intn(len(all)) + 1
	perm := r.Perm(len(all))
	subset := make([]string, n)
	for i := 0; i < n; i++ {
		subset[i] = all[perm[i]]
	}
	return subset
}

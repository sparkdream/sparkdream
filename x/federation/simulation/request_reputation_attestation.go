package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgRequestReputationAttestation(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need an active Spark Dream peer for reputation queries
		peer, err := getOrCreateActivePeer(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestReputationAttestation{}), "failed to get/create active peer"), nil, nil
		}

		// Simulate creating a cached attestation (IBC would normally handle this)
		attestation := types.ReputationAttestation{
			LocalAddress:     addr,
			PeerId:           peer.Id,
			RemoteAddress:    simtypes.RandStringOfLength(r, 20),
			RemoteTrustLevel: uint32(r.Intn(4) + 1),
			LocalTrustCredit: uint32(r.Intn(2)),
			AttestedAt:       ctx.BlockTime().Unix(),
			ExpiresAt:        ctx.BlockTime().Unix() + int64(types.DefaultParams().AttestationTtl.Seconds()),
		}

		if err := k.RepAttestations.Set(ctx, collections.Join(addr, peer.Id), attestation); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestReputationAttestation{}), "failed to set attestation"), nil, nil
		}

		// Set expiration index
		_ = k.AttestationExp.Set(ctx, collections.Join3(attestation.ExpiresAt, addr, peer.Id))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestReputationAttestation{}), "ok (direct keeper call)"), nil, nil
	}
}

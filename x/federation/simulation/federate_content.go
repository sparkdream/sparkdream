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

func SimulateMsgFederateContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need an active peer to federate to
		peer, err := getOrCreateActivePeer(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFederateContent{}), "failed to get/create active peer"), nil, nil
		}

		// Record an outbound attestation
		attestID, err := k.OutboundAttestSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFederateContent{}), "failed to get attest ID"), nil, nil
		}

		attestation := types.OutboundAttestation{
			Id:             attestID,
			PeerId:         peer.Id,
			ContentType:    randomContentType(r),
			LocalContentId: simtypes.RandStringOfLength(r, 8),
			Creator:        addr,
			SubmittedBy:    addr,
			PublishedAt:    ctx.BlockTime().Unix(),
		}

		if err := k.OutboundAttestations.Set(ctx, attestID, attestation); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFederateContent{}), "failed to set attestation"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFederateContent{}), "ok (direct keeper call)"), nil, nil
	}
}

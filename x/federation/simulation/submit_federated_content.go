package simulation

import (
	"encoding/hex"
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgSubmitFederatedContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need an active bridge for the operator
		bridge, err := getOrCreateActiveBridge(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "failed to get/create active bridge"), nil, nil
		}

		contentID, err := k.ContentSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "failed to get content ID"), nil, nil
		}

		hash := randomContentHash(r)
		content := types.FederatedContent{
			Id:              contentID,
			PeerId:          bridge.PeerId,
			RemoteContentId: fmt.Sprintf("remote-%d", r.Intn(100000)),
			ContentType:     randomContentType(r),
			CreatorIdentity: fmt.Sprintf("user@%s", bridge.PeerId[:8]),
			CreatorName:     randomCreatorName(r),
			Title:           randomContentTitle(r),
			Body:            randomContentBody(r),
			RemoteCreatedAt: ctx.BlockTime().Unix() - int64(r.Intn(86400)),
			ReceivedAt:      ctx.BlockTime().Unix(),
			SubmittedBy:     addr,
			Status:          types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION,
			ExpiresAt:       ctx.BlockTime().Unix() + int64(types.DefaultParams().ContentTtl.Seconds()),
			ContentHash:     hash,
		}

		if err := k.Content.Set(ctx, contentID, content); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "failed to set content"), nil, nil
		}
		_ = k.ContentByPeer.Set(ctx, collections.Join(bridge.PeerId, contentID))
		_ = k.ContentByType.Set(ctx, collections.Join(content.ContentType, contentID))
		_ = k.ContentByCreator.Set(ctx, collections.Join(content.CreatorIdentity, contentID))
		_ = k.ContentByHash.Set(ctx, hex.EncodeToString(hash), contentID)
		_ = k.ContentExpiration.Set(ctx, collections.Join(content.ExpiresAt, contentID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "ok (direct keeper call)"), nil, nil
	}
}

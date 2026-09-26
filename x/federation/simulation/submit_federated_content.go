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

		// The handler admits only authors the peer's policy lets in (allowed
		// identities and curation collection): skip where it would refuse.
		creator := fmt.Sprintf("@user@%s", bridge.PeerId)
		if policy, err := k.PeerPolicies.Get(ctx, bridge.PeerId); err != nil || k.CheckAuthorAdmitted(ctx, policy, creator) != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "author not admitted by the peer policy"), nil, nil
		}

		hash := randomContentHash(r)
		// A content_uri and creator handle on the peer's own host, as
		// MsgSubmitFederatedContent requires of ActivityPub peers.
		contentURI := fmt.Sprintf("https://%s/users/sim/statuses/%d", bridge.PeerId, r.Intn(1_000_000))

		// Sometimes anchor an edit instead: a new version of one of this
		// operator's still-pending records on this peer, which the keeper
		// would retire to SUPERSEDED. Mirrors the handler's preconditions
		// (same operator, peer and non-empty content_uri; not yet
		// superseded), so the state it builds is one the handler could
		// have produced.
		var predecessor *types.FederatedContent
		if r.Intn(4) == 0 {
			predecessor = findSupersedable(ctx, k, addr, bridge.PeerId)
			if predecessor != nil {
				contentURI = predecessor.ContentUri
			}
		}

		content := types.FederatedContent{
			Id:              contentID,
			PeerId:          bridge.PeerId,
			RemoteContentId: fmt.Sprintf("remote-%d", r.Intn(100000)),
			ContentType:     randomContentType(r),
			CreatorIdentity: creator,
			ContentUri:      contentURI,
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

		if predecessor != nil {
			content.Supersedes = &types.ContentRef{ContentId: predecessor.Id}
		}
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "failed to set content"), nil, nil
		}
		if predecessor != nil {
			predecessor.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED
			predecessor.SupersededBy = contentID
			_ = k.Content.Set(ctx, predecessor.Id, *predecessor)
		}
		_ = k.ContentByPeer.Set(ctx, collections.Join(bridge.PeerId, contentID))
		_ = k.ContentByType.Set(ctx, collections.Join(content.ContentType, contentID))
		_ = k.ContentByCreator.Set(ctx, collections.Join(content.CreatorIdentity, contentID))
		_ = k.ContentByHash.Set(ctx, hex.EncodeToString(hash), contentID)
		_ = k.ContentExpiration.Set(ctx, collections.Join(content.ExpiresAt, contentID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitFederatedContent{}), "ok (direct keeper call)"), nil, nil
	}
}

// findSupersedable returns one of operator's PENDING_VERIFICATION records
// on peerID with a content_uri and no successor yet, or nil.
func findSupersedable(ctx sdk.Context, k keeper.Keeper, operator, peerID string) *types.FederatedContent {
	var found *types.FederatedContent
	_ = k.Content.Walk(ctx, nil, func(_ uint64, c types.FederatedContent) (bool, error) {
		if c.SubmittedBy == operator && c.PeerId == peerID && c.ContentUri != "" && c.SupersededBy == 0 &&
			c.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION {
			found = &c
			return true, nil
		}
		return false, nil
	})
	return found
}

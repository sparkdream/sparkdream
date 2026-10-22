package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/artifact/types"
)

// EditionView blanks a hidden edition's content (metadata and claim scope).
func EditionView(classStatus types.ContentStatus, e types.Edition) types.EditionView {
	view := types.EditionView{Edition: e, MetadataHash: types.MetadataHash(e.Metadata), ClassStatus: classStatus}
	if classStatus == types.ContentStatus_CONTENT_STATUS_HIDDEN || e.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		view.Edition.Metadata = types.TokenMetadata{}
		view.Edition.Claim.Scope = ""
		view.Withheld = true
	}
	return view
}

func balanceView(e types.Edition, b types.Balance) types.BalanceView {
	return types.BalanceView{Balance: b, Claimable: b.Claimable(e.RewardPerUnit).String()}
}

// editionPrefix paginates a (class, edition, x) index under one edition.
func editionPrefix[K3 any](classID, editionID uint64) func(o *query.CollectionsPaginateOptions[collections.Triple[uint64, uint64, K3]]) {
	return func(o *query.CollectionsPaginateOptions[collections.Triple[uint64, uint64, K3]]) {
		pfx := collections.TripleSuperPrefix[uint64, uint64, K3](classID, editionID)
		o.Prefix = &pfx
	}
}

func (q queryServer) editionCached(ctx context.Context, cache map[[2]uint64]types.Edition, classID, editionID uint64) types.Edition {
	key := [2]uint64{classID, editionID}
	if e, ok := cache[key]; ok {
		return e
	}
	e, _ := q.k.Editions.Get(ctx, collections.Join(classID, editionID))
	if e.RewardPerUnit.IsNil() {
		e.RewardPerUnit = math.ZeroInt()
	}
	cache[key] = e
	return e
}

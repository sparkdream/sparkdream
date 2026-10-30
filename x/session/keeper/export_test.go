package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RewriteSignerField exposes rewriteSignerField to keeper_test.
func RewriteSignerField(msg sdk.Msg, granter string) error {
	return rewriteSignerField(msg, granter)
}

// RewriteAndCheckSigner exposes rewriteAndCheckSigner to keeper_test.
func RewriteAndCheckSigner(cdc codec.Codec, msg sdk.Msg, granter string, granterBz []byte) error {
	return rewriteAndCheckSigner(cdc, msg, granter, granterBz)
}

// PruneStaleEpochBuckets exposes pruneStaleEpochBuckets to keeper_test.
func (k Keeper) PruneStaleEpochBuckets(ctx context.Context, grantID uint64, currentDay int64) {
	k.pruneStaleEpochBuckets(ctx, grantID, currentDay)
}

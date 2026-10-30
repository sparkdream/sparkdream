package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// GetClass is the read-only class lookup exposed to other modules.
func (k Keeper) GetClass(ctx context.Context, classID uint64) (types.Class, bool, error) {
	c, err := k.Classes.Get(ctx, classID)
	if errors.Is(err, collections.ErrNotFound) {
		return c, false, nil
	}
	return c, err == nil, err
}

// GetToken is the read-only token lookup exposed to other modules.
func (k Keeper) GetToken(ctx context.Context, classID, tokenID uint64) (types.Token, bool, error) {
	t, err := k.Tokens.Get(ctx, collections.Join(classID, tokenID))
	if errors.Is(err, collections.ErrNotFound) {
		return t, false, nil
	}
	return t, err == nil, err
}

// GetOwner returns a token's owner.
func (k Keeper) GetOwner(ctx context.Context, classID, tokenID uint64) (sdk.AccAddress, error) {
	t, err := k.getToken(ctx, classID, tokenID)
	if err != nil {
		return nil, err
	}
	return k.addressCodec.StringToBytes(t.Owner)
}

// IsImpactCertificate reports whether an edition carries an impact claim
// and is visible (x/season nominations, docs/x-artifact-spec.md §7.21).
func (k Keeper) IsImpactCertificate(ctx context.Context, classID, editionID uint64) bool {
	e, err := k.Editions.Get(ctx, collections.Join(classID, editionID))
	if err != nil || e.Claim.IsEmpty() || e.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return false
	}
	c, err := k.Classes.Get(ctx, classID)
	return err == nil && c.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN
}

// ImpactCertificateInfo returns the creator and creation time (unix
// seconds) of a visible impact certificate, for x/season's retro-PGF
// self-stake ban and created-this-season check.
func (k Keeper) ImpactCertificateInfo(ctx context.Context, classID, editionID uint64) (string, int64, bool) {
	if !k.IsImpactCertificate(ctx, classID, editionID) {
		return "", 0, false
	}
	e, err := k.Editions.Get(ctx, collections.Join(classID, editionID))
	if err != nil {
		return "", 0, false
	}
	return e.Creator, e.CreatedAt, true
}

package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

// Page limit cap for every list query (docs/x-artifact-spec.md §6).
const maxPageLimit = 100

// ClassView blanks a hidden class's content fields (§7.11).
func ClassView(c types.Class) types.ClassView {
	if c.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return types.ClassView{Class: c}
	}
	c.Name, c.Description, c.Uri, c.UriHash, c.TokenUriBase = "", "", "", "", ""
	return types.ClassView{Class: c, Withheld: true}
}

// TokenView blanks a token's metadata when it, its class or (treeHidden)
// a token it is nested in is hidden, and otherwise picks the displayed
// metadata from stored state (media version, schedule stage or stored
// metadata). Renderers run only in the single-token query
// (Keeper.tokenView).
func TokenView(ctx context.Context, classStatus types.ContentStatus, t types.Token, treeHidden bool) types.TokenView {
	hash := types.MetadataHash(t.Metadata)
	view := types.TokenView{Token: t, MetadataHash: hash, ClassStatus: classStatus, Expired: types.IsExpired(t, now(ctx))}
	if treeHidden || classStatus == types.ContentStatus_CONTENT_STATUS_HIDDEN || t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		view.Token.Metadata = types.TokenMetadata{}
		view.Token.Schedule = nil
		view.Token.MediaVersions = nil
		view.Withheld = true
		return view
	}
	view.DisplayedMetadata, view.DisplaySource = types.StoredDisplay(t, now(ctx))
	return view
}

func (q queryServer) classStatus(ctx context.Context, cache map[uint64]types.ContentStatus, classID uint64) types.ContentStatus {
	if s, ok := cache[classID]; ok {
		return s
	}
	s := types.ContentStatus_CONTENT_STATUS_UNSPECIFIED
	if c, err := q.k.Classes.Get(ctx, classID); err == nil {
		s = c.Status
	}
	cache[classID] = s
	return s
}

// inboxItem builds the client view of a pending item, or false if the item
// is gone or already expired (queries never write state, so expired items
// the EndBlocker has not drained are filtered here).
func (q queryServer) inboxItem(ctx context.Context, classID, tokenID uint64, kind types.InboxKind) (types.InboxItem, bool) {
	t := now(ctx)
	key := collections.Join(classID, tokenID)
	item := types.InboxItem{Kind: kind, ClassId: classID, TokenId: tokenID}
	classStatus := types.ContentStatus_CONTENT_STATUS_UNSPECIFIED
	if c, err := q.k.Classes.Get(ctx, classID); err == nil {
		classStatus = c.Status
	}
	switch kind {
	case types.InboxKind_INBOX_KIND_UNIT_TRANSFER, types.InboxKind_INBOX_KIND_UNIT_MINT:
		pu, err := q.k.PendingUnits.Get(ctx, tokenID)
		if err != nil || pu.ExpiresAt <= t {
			return item, false
		}
		e, err := q.k.Editions.Get(ctx, collections.Join(pu.ClassId, pu.EditionId))
		if err != nil {
			return item, false
		}
		view := EditionView(q.classStatus(ctx, map[uint64]types.ContentStatus{}, pu.ClassId), e)
		item.From, item.To, item.CreatedAt, item.ExpiresAt = pu.From, pu.To, pu.CreatedAt, pu.ExpiresAt
		item.UnitClassId, item.EditionId, item.Amount = pu.ClassId, pu.EditionId, pu.Amount
		item.Metadata, item.MetadataHash, item.Withheld = view.Edition.Metadata, view.MetadataHash, view.Withheld
	case types.InboxKind_INBOX_KIND_TRANSFER:
		pt, err := q.k.PendingTransfers.Get(ctx, key)
		if err != nil || pt.ExpiresAt <= t {
			return item, false
		}
		tok, err := q.k.Tokens.Get(ctx, key)
		if err != nil {
			return item, false
		}
		view := TokenView(ctx, classStatus, tok, q.k.treeWithheld(ctx, tok))
		item.From, item.To, item.CreatedAt, item.ExpiresAt = pt.From, pt.To, pt.CreatedAt, pt.ExpiresAt
		item.Metadata, item.MetadataHash, item.Withheld = view.Token.Metadata, view.MetadataHash, view.Withheld
	default:
		pm, err := q.k.PendingMints.Get(ctx, key)
		if err != nil || pm.ExpiresAt <= t {
			return item, false
		}
		item.From, item.To, item.CreatedAt, item.ExpiresAt = pm.Minter, pm.To, pm.CreatedAt, pm.ExpiresAt
		item.MetadataHash = types.MetadataHash(pm.Metadata)
		if classStatus == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			item.Withheld = true
		} else {
			item.Metadata = pm.Metadata
		}
	}
	return item, true
}

func notFound(err error, what string) error {
	if errors.Is(err, collections.ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s not found", what)
	}
	return status.Error(codes.Internal, err.Error())
}

var errInvalidRequest = status.Error(codes.InvalidArgument, "invalid request")

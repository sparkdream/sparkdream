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

// TokenView blanks a token's metadata when it or its class is hidden.
func TokenView(classStatus types.ContentStatus, t types.Token) types.TokenView {
	hash := types.MetadataHash(t.Metadata)
	if classStatus == types.ContentStatus_CONTENT_STATUS_HIDDEN || t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		t.Metadata = types.TokenMetadata{}
		return types.TokenView{Token: t, Withheld: true, MetadataHash: hash, ClassStatus: classStatus}
	}
	return types.TokenView{Token: t, MetadataHash: hash, ClassStatus: classStatus}
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
	case types.InboxKind_INBOX_KIND_TRANSFER:
		pt, err := q.k.PendingTransfers.Get(ctx, key)
		if err != nil || pt.ExpiresAt <= t {
			return item, false
		}
		tok, err := q.k.Tokens.Get(ctx, key)
		if err != nil {
			return item, false
		}
		view := TokenView(classStatus, tok)
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

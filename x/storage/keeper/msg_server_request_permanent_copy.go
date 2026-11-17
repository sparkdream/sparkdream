package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) RequestPermanentCopy(ctx context.Context, msg *types.MsgRequestPermanentCopy) (*types.MsgRequestPermanentCopyResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, types.ErrModuleDisabled
	}
	b, err := k.Backends.Get(ctx, msg.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(msg.Backend)
	}
	if !b.Enabled {
		return nil, types.ErrBackendDisabled.Wrap(msg.Backend)
	}
	trust, err := k.eligibleMember(ctx, msg.Requester, p)
	if err != nil {
		return nil, err
	}
	if err := types.ValidateHash(msg.Sha256); err != nil {
		return nil, err
	}
	if flagged, err := k.isFlagged(ctx, msg.Sha256); err != nil {
		return nil, err
	} else if flagged {
		return nil, types.ErrHashRemoved
	}
	t, ok := b.FindType(msg.ContentType)
	if !ok {
		return nil, types.ErrContentTypeNotAllowed.Wrap(msg.ContentType)
	}
	if msg.SizeBytes == 0 {
		return nil, types.ErrInvalidSize
	}
	if msg.SizeBytes > t.MaxSize {
		return nil, types.ErrFileTooLarge.Wrapf("%d > %d", msg.SizeBytes, t.MaxSize)
	}
	if t.Scanned {
		if _, err := k.scannerQuorum(ctx, trust, p); err != nil {
			return nil, err
		}
	}
	if err := types.ValidateSourceURL(msg.Url, p.MaxUrlBytes); err != nil {
		return nil, err
	}

	// One request per (backend, sha256).
	existing, err := k.LiveByHash.Get(ctx, collections.Join(b.Name, msg.Sha256))
	if err == nil {
		if prev, gerr := k.GetRequest(ctx, existing); gerr == nil && prev.Status == types.RequestStatus_REQUEST_STATUS_SETTLED {
			return nil, types.ErrAlreadyStored.Wrapf("request %d", existing)
		}
		return nil, types.ErrAlreadyRequested.Wrapf("request %d", existing)
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}

	// Do not burn a fee for a request nobody can take.
	if ok, err := k.hasClaimableOperator(ctx, b); err != nil {
		return nil, err
	} else if !ok {
		return nil, types.ErrNoActiveOperator
	}
	if n, err := k.countOpenRequests(ctx, msg.Requester, p.MaxOpenRequestsPerRequester); err != nil {
		return nil, err
	} else if n >= p.MaxOpenRequestsPerRequester {
		return nil, types.ErrTooManyOpenRequests
	}

	copyFee := b.CopyFee(msg.SizeBytes)
	total := copyFee.Add(p.RequestFee)
	if msg.MaxFee.IsNil() || total.GT(msg.MaxFee) {
		return nil, types.ErrFeeExceedsMax.Wrapf("total %s", total)
	}
	if err := k.charge(ctx, msg.Requester, total); err != nil {
		return nil, err
	}
	if err := k.burn(ctx, p.RequestFee); err != nil {
		return nil, err
	}

	id, err := k.RequestSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	id++ // the first id is 1
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	r := types.Request{
		Id:                  id,
		Requester:           msg.Requester,
		RequesterTrustLevel: trust,
		Backend:             b.Name,
		Sha256:              msg.Sha256,
		SizeBytes:           msg.SizeBytes,
		ContentType:         msg.ContentType,
		Scanned:             t.Scanned,
		Sources:             []types.Source{{Url: msg.Url, AddedBy: msg.Requester, AddedAt: now}},
		CopyFee:             copyFee,
		Status:              types.RequestStatus_REQUEST_STATUS_PENDING,
		CreatedAt:           now,
		OpenedAt:            now,
		Deadline:            now + p.RequestTtlBlocks,
		PaidAmount:          math.ZeroInt(),
	}
	if err := k.saveRequest(ctx, nil, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("request_created", r,
		sdk.NewAttribute("requester", r.Requester),
		sdk.NewAttribute("content_type", r.ContentType),
		sdk.NewAttribute("size_bytes", u64(r.SizeBytes)),
		sdk.NewAttribute("scanned", boolStr(r.Scanned)),
		sdk.NewAttribute("url", msg.Url),
		sdk.NewAttribute("copy_fee", copyFee.String()),
		sdk.NewAttribute("deadline", i64(r.Deadline)),
	))
	return &types.MsgRequestPermanentCopyResponse{RequestId: id}, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

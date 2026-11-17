package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func (k msgServer) AddSource(ctx context.Context, msg *types.MsgAddSource) (*types.MsgAddSourceResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	r, err := k.GetRequest(ctx, msg.RequestId)
	if err != nil {
		return nil, err
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_PENDING:
		if now > r.Deadline {
			return nil, types.ErrDeadlinePassed
		}
	case types.RequestStatus_REQUEST_STATUS_CLAIMED:
	default:
		return nil, types.ErrWrongStatus.Wrap(r.Status.String())
	}
	if _, err := k.eligibleMember(ctx, msg.Member, p); err != nil {
		return nil, err
	}
	if uint64(len(r.Sources)) >= uint64(p.MaxSourcesPerRequest) {
		return nil, types.ErrTooManySources
	}
	if err := types.ValidateSourceURL(msg.Url, p.MaxUrlBytes); err != nil {
		return nil, err
	}
	member := k.canonical(ctx, msg.Member)
	for _, s := range r.Sources {
		if s.Url == msg.Url {
			return nil, types.ErrSourceExists.Wrap("address already listed")
		}
		if k.canonical(ctx, s.AddedBy) == member {
			return nil, types.ErrSourceExists.Wrap("one source per member")
		}
	}
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(r.Backend)
	}
	if ok, err := k.hasClaimableOperator(ctx, b); err != nil {
		return nil, err
	} else if !ok {
		return nil, types.ErrNoActiveOperator
	}
	if msg.MaxFee.IsNil() || p.RequestFee.GT(msg.MaxFee) {
		return nil, types.ErrFeeExceedsMax.Wrapf("fee %s", p.RequestFee)
	}
	if err := k.chargeAndBurn(ctx, msg.Member, p.RequestFee); err != nil {
		return nil, err
	}
	old := r
	r.Sources = append(append([]types.Source(nil), r.Sources...), types.Source{Url: msg.Url, AddedBy: msg.Member, AddedAt: now})
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("source_added", r,
		sdk.NewAttribute("member", msg.Member),
		sdk.NewAttribute("url", msg.Url),
	))
	return &types.MsgAddSourceResponse{}, nil
}

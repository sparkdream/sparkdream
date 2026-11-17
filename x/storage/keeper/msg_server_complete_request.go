package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

func (k msgServer) CompleteRequest(ctx context.Context, msg *types.MsgCompleteRequest) (*types.MsgCompleteRequestResponse, error) {
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
	if r.Status != types.RequestStatus_REQUEST_STATUS_CLAIMED || r.Operator != msg.Operator {
		return nil, types.ErrNotClaimant
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if now > r.ClaimExpiresAt {
		return nil, types.ErrClaimExpired
	}
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(r.Backend)
	}
	// An operator whose bond is still held may finish what it claimed, even
	// when it can no longer claim. One with no live record cannot: nothing
	// is left to answer for its word.
	op, live := k.liveOperator(ctx, msg.Operator, b.ServiceType)
	if !live {
		return nil, types.ErrOperatorNotActive.Wrap("no live operator record")
	}
	switch op.Status {
	case servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING:
	default:
		return nil, types.ErrOperatorNotActive.Wrap(op.Status.String())
	}
	if err := types.ValidateLocator(b.LocatorFormat, msg.Locator); err != nil {
		return nil, err
	}
	if r.Scanned {
		if uint64(len(msg.Attestations)) < uint64(r.RequiredAttestations) {
			return nil, types.ErrInvalidAttestations.Wrapf("need at least %d", r.RequiredAttestations)
		}
		if uint64(len(msg.Attestations)) > uint64(p.MaxAttestationsPerCompletion) {
			return nil, types.ErrInvalidAttestations.Wrapf("at most %d", p.MaxAttestationsPerCompletion)
		}
		seen := make(map[string]bool, len(msg.Attestations))
		for _, a := range msg.Attestations {
			if types.ValidateHash(a) != nil {
				return nil, types.ErrInvalidAttestations.Wrap("each attestation is 64 lowercase hex characters")
			}
			if seen[a] {
				return nil, types.ErrInvalidAttestations.Wrap("attestations must be distinct")
			}
			seen[a] = true
		}
		if err := types.ValidateLocator(b.LocatorFormat, msg.VerdictsLocator); err != nil {
			return nil, types.ErrInvalidAttestations.Wrap("verdicts_locator: " + err.Error())
		}
		if msg.VerdictsLocator == msg.Locator {
			return nil, types.ErrInvalidAttestations.Wrap("verdicts_locator must differ from the locator")
		}
	} else if len(msg.Attestations) != 0 || msg.VerdictsLocator != "" {
		return nil, types.ErrInvalidAttestations.Wrap("the request is not scanned")
	}
	// A completion is a closing power; cap it like the others.
	if err := k.bumpCapped(ctx, msg.Operator, types.CounterKind_COUNTER_KIND_COMPLETION, p.MaxCompletionsPerOperatorPerWindow, now, p); err != nil {
		return nil, err
	}
	old := r
	r.Status = types.RequestStatus_REQUEST_STATUS_COMPLETED
	r.Locator = msg.Locator
	r.Attestations = msg.Attestations
	r.VerdictsLocator = msg.VerdictsLocator
	r.CompletedAt = now
	r.SettleAt = now + p.SettleWindowBlocks
	r.Disputed = false
	if err := k.saveRequest(ctx, &old, r, p); err != nil {
		return nil, err
	}
	emit(ctx, requestEvent("request_completed", r,
		sdk.NewAttribute("operator", r.Operator),
		sdk.NewAttribute("locator", r.Locator),
		sdk.NewAttribute("attestations", strings.Join(r.Attestations, ",")),
		sdk.NewAttribute("verdicts_locator", r.VerdictsLocator),
		sdk.NewAttribute("settle_at", i64(r.SettleAt)),
	))
	return &types.MsgCompleteRequestResponse{}, nil
}

package keeper

import (
	"context"
	"errors"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

// statusKey is the first half of a ByStatus key. A backend name holds only
// [a-z0-9-], so the separator cannot collide.
func statusKey(backend string, status types.RequestStatus) string {
	return backend + "/" + strconv.Itoa(int(status))
}

func sdkHeight(ctx context.Context) int64 { return sdk.UnwrapSDKContext(ctx).BlockHeight() }
func i64(v int64) string                  { return strconv.FormatInt(v, 10) }

// timer is one pending Deadlines entry of a request.
type timer struct {
	kind   uint32
	height int64
}

// timersOf lists the timers a request's status calls for (spec section 5.1).
// A request has at most one of each kind.
func timersOf(r types.Request, p types.Params) []timer {
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_PENDING:
		return []timer{{types.DeadlineExpiry, r.Deadline}}
	case types.RequestStatus_REQUEST_STATUS_CLAIMED:
		return []timer{{types.DeadlineClaim, r.ClaimExpiresAt}}
	case types.RequestStatus_REQUEST_STATUS_COMPLETED:
		return []timer{{types.DeadlineSettle, r.SettleAt}}
	case types.RequestStatus_REQUEST_STATUS_SETTLED:
		return nil
	}
	// Terminal. While a flagged fee is held the request waits for the
	// forfeit; it is pruned only after that.
	if r.ForfeitAt != 0 {
		return []timer{{types.DeadlineForfeit, r.ForfeitAt}}
	}
	return []timer{{types.DeadlinePrune, r.ClosedAt + p.TerminalRetentionBlocks}}
}

// GetRequest loads a request.
func (k Keeper) GetRequest(ctx context.Context, id uint64) (types.Request, error) {
	r, err := k.Requests.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Request{}, types.ErrRequestNotFound.Wrapf("id %d", id)
		}
		return types.Request{}, err
	}
	return r, nil
}

// saveRequest writes a request and moves its indexes and timers from what
// old called for to what r calls for. old is nil for a new request. Every
// status change goes through here, so the indexes cannot drift.
func (k Keeper) saveRequest(ctx context.Context, old *types.Request, r types.Request, p types.Params) error {
	if old != nil {
		if err := k.unindex(ctx, *old, p); err != nil {
			return err
		}
	}
	if err := k.index(ctx, r, p); err != nil {
		return err
	}
	return k.Requests.Set(ctx, r.Id, r)
}

func (k Keeper) index(ctx context.Context, r types.Request, p types.Params) error {
	if err := k.ByStatus.Set(ctx, collections.Join(statusKey(r.Backend, r.Status), r.Id)); err != nil {
		return err
	}
	if err := k.ByRequester.Set(ctx, collections.Join(r.Requester, r.Id)); err != nil {
		return err
	}
	if r.Status.IsOpen() {
		if err := k.OpenByRequester.Set(ctx, collections.Join(r.Requester, r.Id)); err != nil {
			return err
		}
	}
	if r.Status.IsLive() {
		if err := k.LiveByHash.Set(ctx, collections.Join(r.Backend, r.Sha256), r.Id); err != nil {
			return err
		}
	}
	switch r.Status {
	case types.RequestStatus_REQUEST_STATUS_CLAIMED:
		if err := k.ClaimsByOperator.Set(ctx, collections.Join(r.Operator, r.Id)); err != nil {
			return err
		}
		fallthrough
	case types.RequestStatus_REQUEST_STATUS_COMPLETED:
		if err := k.ByOperator.Set(ctx, collections.Join(r.Operator, r.Id)); err != nil {
			return err
		}
	}
	for _, t := range timersOf(r, p) {
		if err := k.Deadlines.Set(ctx, collections.Join3(t.height, r.Id, t.kind)); err != nil {
			return err
		}
	}
	return nil
}

// unindex removes everything index wrote for r. Removing a key that is not
// there is not an error: a timer may have been moved by a retry.
func (k Keeper) unindex(ctx context.Context, r types.Request, p types.Params) error {
	if err := k.ByStatus.Remove(ctx, collections.Join(statusKey(r.Backend, r.Status), r.Id)); err != nil {
		return err
	}
	if err := k.ByRequester.Remove(ctx, collections.Join(r.Requester, r.Id)); err != nil {
		return err
	}
	if err := k.OpenByRequester.Remove(ctx, collections.Join(r.Requester, r.Id)); err != nil {
		return err
	}
	if r.Status.IsLive() {
		key := collections.Join(r.Backend, r.Sha256)
		if id, err := k.LiveByHash.Get(ctx, key); err == nil && id == r.Id {
			if err := k.LiveByHash.Remove(ctx, key); err != nil {
				return err
			}
		}
	}
	if r.Operator != "" {
		if err := k.ClaimsByOperator.Remove(ctx, collections.Join(r.Operator, r.Id)); err != nil {
			return err
		}
		if err := k.ByOperator.Remove(ctx, collections.Join(r.Operator, r.Id)); err != nil {
			return err
		}
	}
	for _, t := range timersOf(r, p) {
		if err := k.Deadlines.Remove(ctx, collections.Join3(t.height, r.Id, t.kind)); err != nil {
			return err
		}
	}
	return nil
}

// deleteRequest prunes a closed request.
func (k Keeper) deleteRequest(ctx context.Context, r types.Request, p types.Params) error {
	if err := k.unindex(ctx, r, p); err != nil {
		return err
	}
	return k.Requests.Remove(ctx, r.Id)
}

// clearClaim resets the fields a claim set.
func clearClaim(r *types.Request) {
	r.Operator = ""
	r.Controller = ""
	r.ClaimedAt = 0
	r.ClaimExpiresAt = 0
	r.ClaimExtended = false
	r.RequiredAttestations = 0
}

// clearCompletion resets the fields a completion set.
func clearCompletion(r *types.Request) {
	r.Locator = ""
	r.Attestations = nil
	r.VerdictsLocator = ""
	r.CompletedAt = 0
	r.SettleAt = 0
	r.Disputed = false
}

// closeRequest moves r to a terminal status.
func closeRequest(r *types.Request, status types.RequestStatus, reason types.ClosedReason, now int64) {
	r.Status = status
	r.ClosedReason = reason
	r.ClosedAt = now
}

// bar adds an operator to the request's barred list once.
func bar(r *types.Request, operator string) {
	if operator == "" {
		return
	}
	for _, b := range r.Barred {
		if b == operator {
			return
		}
	}
	r.Barred = append(r.Barred, operator)
}

// restart returns r to PENDING as a fresh attempt: a void, or a flag lifted
// from a request that was only claimed. Verdicts scanned before now no
// longer count.
func restart(r *types.Request, now int64, p types.Params) {
	clearClaim(r)
	clearCompletion(r)
	r.Status = types.RequestStatus_REQUEST_STATUS_PENDING
	r.ClosedReason = types.ClosedReason_CLOSED_REASON_UNSPECIFIED
	r.ClosedAt = 0
	r.ForfeitAt = 0
	r.OpenedAt = now
	r.Deadline = now + p.RequestTtlBlocks
}

// voidToPending applies a void that restarts the request: the lists are
// cleared so a request filled by earlier lapses is claimable again, and only
// the voided operator is barred.
func voidToPending(r *types.Request, now int64, p types.Params) {
	operator := r.Operator
	restart(r, now, p)
	r.DeclinedBy = nil
	r.Barred = nil
	bar(r, operator)
	r.PaidTo = ""
	r.PaidAmount = math.ZeroInt()
}

// releaseClaim returns a CLAIMED request to PENDING after a lapse or the
// claimant's decline. The deadline is not moved.
func releaseClaim(r *types.Request) {
	operator := r.Operator
	clearClaim(r)
	r.Status = types.RequestStatus_REQUEST_STATUS_PENDING
	bar(r, operator)
}

// requestEvent builds an event carrying the attributes every request event has.
func requestEvent(name string, r types.Request, attrs ...sdk.Attribute) sdk.Event {
	base := []sdk.Attribute{
		sdk.NewAttribute("request_id", u64(r.Id)),
		sdk.NewAttribute("backend", r.Backend),
		sdk.NewAttribute("sha256", r.Sha256),
	}
	return sdk.NewEvent("storage."+name, append(base, attrs...)...)
}

func emit(ctx context.Context, ev sdk.Event) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(ev)
}

// isFlagged reports whether a hash is in RemovedHashes.
func (k Keeper) isFlagged(ctx context.Context, sha256 string) (bool, error) {
	return k.RemovedHashes.Has(ctx, sha256)
}

// expire closes a PENDING request whose deadline has passed and refunds it.
func (k Keeper) expire(ctx context.Context, r *types.Request, now int64) error {
	if err := k.refund(ctx, r); err != nil {
		return err
	}
	closeRequest(r, types.RequestStatus_REQUEST_STATUS_EXPIRED, types.ClosedReason_CLOSED_REASON_EXPIRED, now)
	emit(ctx, requestEvent("request_expired", *r))
	return nil
}

// countOpenRequests counts a member's PENDING, CLAIMED and COMPLETED
// requests across every address it has used, stopping at limit.
func (k Keeper) countOpenRequests(ctx context.Context, member string, limit uint32) (uint32, error) {
	var n uint32
	for _, addr := range k.late.rep.AllAddressesOf(ctx, member) {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		err := k.OpenByRequester.Walk(ctx, rng, func(collections.Pair[string, uint64]) (bool, error) {
			n++
			return n >= limit, nil
		})
		if err != nil {
			return 0, err
		}
		if n >= limit {
			break
		}
	}
	return n, nil
}

// countClaims counts an operator's CLAIMED requests, stopping at limit.
func (k Keeper) countClaims(ctx context.Context, operator string, limit uint32) (uint32, error) {
	var n uint32
	rng := collections.NewPrefixedPairRange[string, uint64](operator)
	err := k.ClaimsByOperator.Walk(ctx, rng, func(collections.Pair[string, uint64]) (bool, error) {
		n++
		return n >= limit, nil
	})
	return n, err
}

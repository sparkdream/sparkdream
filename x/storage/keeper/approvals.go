package keeper

import (
	"context"

	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

// liveOperator returns the operator's live x/service record.
func (k Keeper) liveOperator(ctx context.Context, operator, serviceType string) (servicetypes.Operator, bool) {
	addr, err := k.accAddr(operator)
	if err != nil {
		return servicetypes.Operator{}, false
	}
	return k.late.service.GetOperator(ctx, addr.Bytes(), serviceType)
}

// operatorApproval returns the operator's approval for the backend when it
// still counts: x/service must still name the controller that approved.
func (k Keeper) operatorApproval(ctx context.Context, b types.Backend, operator string) (types.Approval, bool) {
	a, err := k.ApprovedOperators.Get(ctx, collections.Join(b.Name, operator))
	if err != nil {
		return types.Approval{}, false
	}
	op, live := k.liveOperator(ctx, operator, b.ServiceType)
	if !live || op.Controller != a.Controller {
		return types.Approval{}, false
	}
	return a, true
}

// couldClaim reports whether an operator may take requests of the backend
// now: approved, ACTIVE and an active member. Returns the reason otherwise.
func (k Keeper) couldClaim(ctx context.Context, b types.Backend, operator string) (types.Approval, error) {
	a, ok := k.operatorApproval(ctx, b, operator)
	if !ok {
		return types.Approval{}, types.ErrOperatorNotApproved
	}
	addr, err := k.accAddr(operator)
	if err != nil {
		return types.Approval{}, err
	}
	if !k.late.service.IsActiveOperator(ctx, addr, b.ServiceType) {
		return types.Approval{}, types.ErrOperatorNotActive
	}
	if !k.late.rep.IsActiveMember(ctx, addr) {
		return types.Approval{}, types.ErrOperatorNotMember
	}
	return a, nil
}

// hasClaimableOperator reports whether any approved operator of the backend
// could claim now. It reads the approved list, a bounded number of entries.
func (k Keeper) hasClaimableOperator(ctx context.Context, b types.Backend) (bool, error) {
	found := false
	rng := collections.NewPrefixedPairRange[string, string](b.Name)
	err := k.ApprovedOperators.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.Approval) (bool, error) {
		if _, err := k.couldClaim(ctx, b, key.K2()); err == nil {
			found = true
			return true, nil
		}
		return false, nil
	})
	return found, err
}

// countApprovedOperators counts a backend's approvals.
func (k Keeper) countApprovedOperators(ctx context.Context, backend string) (uint32, error) {
	var n uint32
	rng := collections.NewPrefixedPairRange[string, string](backend)
	err := k.ApprovedOperators.Walk(ctx, rng, func(collections.Pair[string, string], types.Approval) (bool, error) {
		n++
		return false, nil
	})
	return n, err
}

// scannerCounts reports whether a scanner's flags and verdicts count here:
// approved, with x/service still naming the approving controller, and ACTIVE.
func (k Keeper) scannerCounts(ctx context.Context, scanner string) bool {
	a, err := k.ApprovedScanners.Get(ctx, scanner)
	if err != nil {
		return false
	}
	op, live := k.liveOperator(ctx, scanner, types.ContentScannerServiceType)
	if !live || op.Controller != a.Controller {
		return false
	}
	addr, err := k.accAddr(scanner)
	if err != nil {
		return false
	}
	return k.late.service.IsActiveOperator(ctx, addr, types.ContentScannerServiceType)
}

// countApprovedScanners counts approvals, and those that count now.
func (k Keeper) countApprovedScanners(ctx context.Context) (total, active uint32, err error) {
	err = k.ApprovedScanners.Walk(ctx, nil, func(scanner string, _ types.Approval) (bool, error) {
		total++
		if k.scannerCounts(ctx, scanner) {
			active++
		}
		return false, nil
	})
	return total, active, err
}

// scannerQuorum returns the number of clean verdicts a request from a member
// at trustLevel needs, and fails when no sound completion could be made: the
// content-scanner quorum reads 0, or fewer approved scanners are active.
func (k Keeper) scannerQuorum(ctx context.Context, trustLevel uint32, p types.Params) (uint32, error) {
	cfg, ok := k.late.service.GetServiceTypeConfig(ctx, types.ContentScannerServiceType)
	if !ok {
		return 0, types.ErrNoScannerQuorum.Wrap("content-scanner service type not found")
	}
	q := types.RequiredAttestations(cfg.AttestationQuorum, cfg.ElevatedAttestationQuorum, trustLevel)
	if q == 0 {
		return 0, types.ErrNoScannerQuorum.Wrap("the content-scanner quorum is 0")
	}
	if q > p.MaxAttestationsPerCompletion {
		return 0, types.ErrNoScannerQuorum.Wrapf("quorum %d is above max_attestations_per_completion", q)
	}
	_, active, err := k.countApprovedScanners(ctx)
	if err != nil {
		return 0, err
	}
	if active < q {
		return 0, types.ErrNoScannerQuorum.Wrapf("%d approved active scanners, quorum %d", active, q)
	}
	return q, nil
}

// eligibleMember applies section 3.3: an active member at the minimum trust
// level. It returns the member's trust level.
func (k Keeper) eligibleMember(ctx context.Context, member string, p types.Params) (uint32, error) {
	addr, err := k.accAddr(member)
	if err != nil {
		return 0, types.ErrRequesterNotPermitted.Wrap("invalid address")
	}
	if !k.late.rep.IsActiveMember(ctx, addr) {
		return 0, types.ErrRequesterNotPermitted.Wrap("not an active member")
	}
	tl, err := k.late.rep.GetTrustLevel(ctx, addr)
	if err != nil {
		return 0, types.ErrRequesterNotPermitted.Wrap(err.Error())
	}
	level := commontypes.Saturate[uint32](int32(tl))
	if level < p.MinRequesterTrustLevel {
		return 0, types.ErrRequesterNotPermitted.Wrapf("trust level %d is below %d", level, p.MinRequesterTrustLevel)
	}
	return level, nil
}

// isBarred reports whether an operator, under its current or a recent
// address, is on the request's barred list.
func (k Keeper) isBarred(ctx context.Context, r types.Request, operator string) bool {
	if len(r.Barred) == 0 {
		return false
	}
	addrs := append([]string{operator}, k.late.rep.RecentAddressesOf(ctx, operator)...)
	for _, b := range r.Barred {
		for _, a := range addrs {
			if a == b {
				return true
			}
		}
	}
	return false
}

// checkUnbondingCovers refuses windows that an operator of the service type
// could outlast by unbonding (spec section 3.5).
func (k Keeper) checkUnbondingCovers(ctx context.Context, p types.Params, serviceType string) error {
	period, ok := k.late.service.UnbondingPeriodBlocks(ctx, serviceType)
	if !ok {
		return types.ErrInvalidBackend.Wrapf("service type %q not found", serviceType)
	}
	if p.HoldSpan() >= period {
		return types.ErrInvalidParams.Wrapf(
			"2 x claim + 2 x (settle + dispute) + flag hold = %d blocks must be below the %d-block unbonding period of %q",
			p.HoldSpan(), period, serviceType)
	}
	return nil
}

// checkParamsAgainstState makes the param checks that read other state.
func (k Keeper) checkParamsAgainstState(ctx context.Context, p types.Params) error {
	if k.late.service == nil {
		return nil // unit tests of the params handler alone
	}
	total, _, err := k.countApprovedScanners(ctx)
	if err != nil {
		return err
	}
	if total > p.MaxApprovedScanners {
		return types.ErrInvalidParams.Wrap("max_approved_scanners is below the number approved")
	}
	return k.Backends.Walk(ctx, nil, func(name string, b types.Backend) (bool, error) {
		if b.DeclinesToClose > p.MaxBarredPerRequest {
			return true, types.ErrInvalidParams.Wrapf("max_barred_per_request is below declines_to_close of %q", name)
		}
		n, err := k.countApprovedOperators(ctx, name)
		if err != nil {
			return true, err
		}
		if n > p.MaxOperatorsPerBackend {
			return true, types.ErrInvalidParams.Wrapf("max_operators_per_backend is below the approvals of %q", name)
		}
		return false, k.checkUnbondingCovers(ctx, p, b.ServiceType)
	})
}

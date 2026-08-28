package keeper

import (
	"context"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The stored federation VerifierActivity record is SLIM: it holds only
// unchallenged_verifications. Everything else — per-kind verification
// counters, verdict streaks, the overturn cooldown, the slash-epoch stamp —
// lives on x/rep's shared RoleActivity record under
// ROLE_TYPE_FEDERATION_VERIFIER. This handler PROJECTS the rep state back
// into the full response shape so clients and e2e scripts keep their field
// paths. The projected fields are never persisted in federation state. Same
// pattern as forum's get-sentinel-activity. See docs/x-rep-spec.md
// (RoleActivity).

// projectVerifierActivity overlays rep's shared accountability data onto the
// slim federation-local record.
func (k Keeper) projectVerifierActivity(ctx context.Context, local types.VerifierActivity) types.VerifierActivityView {
	view := types.VerifierActivityView{
		Address:                   local.Address,
		UnchallengedVerifications: local.UnchallengedVerifications,
	}
	if k.late.repKeeper == nil {
		return view
	}
	ra, err := k.late.repKeeper.GetRoleActivity(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, local.Address)
	if err != nil {
		return view
	}

	kind := reptypes.ActionKindFederationVerify
	view.TotalVerifications = ra.TotalActions[kind]
	view.UpheldVerifications = ra.UpheldActions[kind]
	view.OverturnedVerifications = ra.OverturnedActions[kind]
	view.EpochVerifications = ra.EpochActions[kind]

	view.EpochChallengesResolved = ra.EpochAppealsResolved
	view.ConsecutiveUpheld = ra.ConsecutiveUpheld
	view.ConsecutiveOverturns = ra.ConsecutiveOverturns
	view.OverturnCooldownUntil = ra.OverturnCooldownUntil
	view.LastSlashEpoch = ra.LastSlashEpoch

	// Derived, not stored: an upheld challenge slashes exactly once.
	view.SlashCount = ra.OverturnedActions[kind]
	return view
}

// VerifierActivity returns the per-verifier counter view: federation's slim
// stored record overlaid with x/rep's shared accountability state. The
// generic bond/status record lives in x/rep under
// BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr).
//
// Soft-zeros on a missing record rather than erroring, so callers can probe
// an address without special-casing "never verified anything".
func (q queryServer) VerifierActivity(ctx context.Context, req *types.QueryVerifierActivityRequest) (*types.QueryVerifierActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	activity, err := q.k.VerifierActivity.Get(ctx, req.Address)
	if err != nil {
		activity = types.VerifierActivity{Address: req.Address}
	}
	activity.Address = req.Address

	return &types.QueryVerifierActivityResponse{
		Activity: q.k.projectVerifierActivity(ctx, activity),
	}, nil
}

package keeper

import (
	"context"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The stored forum SentinelActivity record is SLIM (pending-hide count,
// unchallenged hides, curation-proposal lifecycle counters). Everything
// else — streaks, cooldown, accuracy ring, per-action counters — lives on
// x/rep's shared RoleActivity record. These query handlers PROJECT the rep
// state back into the legacy response shape so clients and e2e scripts keep
// their field paths. The projected fields are never persisted in forum
// state. See docs/x-rep-spec.md (RoleActivity).

// projectSentinelActivity overlays the rep-side shared accountability data
// onto a (slim) forum-local record for query responses.
func (k Keeper) projectSentinelActivity(ctx context.Context, local types.SentinelActivity) types.SentinelActivity {
	if k.repKeeper == nil {
		return local
	}
	ra, err := k.repKeeper.GetRoleActivity(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, local.Address)
	if err != nil {
		return local
	}

	local.TotalHides = ra.TotalActions[reptypes.ActionKindForumHide]
	local.UpheldHides = ra.UpheldActions[reptypes.ActionKindForumHide]
	local.OverturnedHides = ra.OverturnedActions[reptypes.ActionKindForumHide]
	local.EpochHides = ra.EpochActions[reptypes.ActionKindForumHide]

	local.TotalLocks = ra.TotalActions[reptypes.ActionKindForumLock]
	local.UpheldLocks = ra.UpheldActions[reptypes.ActionKindForumLock]
	local.OverturnedLocks = ra.OverturnedActions[reptypes.ActionKindForumLock]
	local.EpochLocks = ra.EpochActions[reptypes.ActionKindForumLock]

	local.TotalMoves = ra.TotalActions[reptypes.ActionKindForumMove]
	local.UpheldMoves = ra.UpheldActions[reptypes.ActionKindForumMove]
	local.OverturnedMoves = ra.OverturnedActions[reptypes.ActionKindForumMove]
	local.EpochMoves = ra.EpochActions[reptypes.ActionKindForumMove]

	local.TotalPins = ra.TotalActions[reptypes.ActionKindForumPin]
	local.UpheldPins = ra.UpheldActions[reptypes.ActionKindForumPin]
	local.OverturnedPins = ra.OverturnedActions[reptypes.ActionKindForumPin]
	local.EpochPins = ra.EpochActions[reptypes.ActionKindForumPin]

	local.EpochCurations = ra.EpochActions[reptypes.ActionKindForumCuration]
	local.EpochAppealsFiled = ra.EpochActions[reptypes.ActionKindForumAppealFiled]
	local.EpochAppealsResolved = ra.EpochAppealsResolved

	local.TotalCollectHides = ra.TotalActions[reptypes.ActionKindCollectHide]
	local.UpheldCollectHides = ra.UpheldActions[reptypes.ActionKindCollectHide]
	local.OverturnedCollectHides = ra.OverturnedActions[reptypes.ActionKindCollectHide]
	local.EpochCollectHides = ra.EpochActions[reptypes.ActionKindCollectHide]

	local.ConsecutiveUpheld = ra.ConsecutiveUpheld
	local.ConsecutiveOverturns = ra.ConsecutiveOverturns
	local.OverturnCooldownUntil = ra.OverturnCooldownUntil

	local.AccuracyWindow = make([]*types.AccuracyEpochBucket, 0, len(ra.AccuracyWindow))
	for _, b := range ra.AccuracyWindow {
		if b == nil {
			local.AccuracyWindow = append(local.AccuracyWindow, &types.AccuracyEpochBucket{})
			continue
		}
		local.AccuracyWindow = append(local.AccuracyWindow, &types.AccuracyEpochBucket{
			Epoch: b.Epoch, Upheld: b.Upheld, Overturned: b.Overturned,
		})
	}
	return local
}

func (q queryServer) ListSentinelActivity(ctx context.Context, req *types.QueryAllSentinelActivityRequest) (*types.QueryAllSentinelActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sentinelActivitys, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SentinelActivity,
		req.Pagination,
		func(_ string, value types.SentinelActivity) (types.SentinelActivity, error) {
			return q.k.projectSentinelActivity(ctx, value), nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllSentinelActivityResponse{SentinelActivity: sentinelActivitys, Pagination: pageRes}, nil
}

func (q queryServer) GetSentinelActivity(ctx context.Context, req *types.QueryGetSentinelActivityRequest) (*types.QueryGetSentinelActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// A sentinel who has only acted on other surfaces (e.g. collect) has no
	// forum-local record; project onto an empty one so the shared
	// accountability data is still visible. NotFound only when neither side
	// knows the address.
	val, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		val = types.SentinelActivity{Address: req.Address}
	}
	projected := q.k.projectSentinelActivity(ctx, val)
	if err != nil && isZeroSentinelActivity(projected) {
		return nil, status.Error(codes.NotFound, "not found")
	}

	return &types.QueryGetSentinelActivityResponse{SentinelActivity: projected}, nil
}

// isZeroSentinelActivity reports whether a projected record carries no data
// at all (no forum-local record AND no rep-side activity).
func isZeroSentinelActivity(sa types.SentinelActivity) bool {
	return sa.TotalHides == 0 && sa.TotalLocks == 0 && sa.TotalMoves == 0 &&
		sa.TotalPins == 0 && sa.TotalCollectHides == 0 &&
		sa.TotalProposals == 0 && sa.EpochAppealsResolved == 0 &&
		sa.ConsecutiveUpheld == 0 && sa.ConsecutiveOverturns == 0 &&
		sa.OverturnCooldownUntil == 0 && len(sa.AccuracyWindow) == 0
}

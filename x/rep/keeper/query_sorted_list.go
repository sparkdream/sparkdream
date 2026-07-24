package keeper

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Shared machinery for the sorted list queries (ListProject, ListInitiative
// and their filtered variants). A sorted query cannot page by store key —
// the sort order isn't the store order — so the whole (filtered) set is
// collected, sorted, and paged by offset, with next_key carrying the next
// offset as a decimal string. Direction follows pagination.reverse for every
// key, matching how the unsorted queries already expose direction.

const defaultSortedPageLimit = 100

// collectProjects gathers every project matching the predicate, in id order.
func (q queryServer) collectProjects(ctx context.Context, match func(types.Project) bool) ([]types.Project, error) {
	var out []types.Project
	err := q.k.Project.Walk(ctx, nil, func(_ uint64, project types.Project) (bool, error) {
		if match == nil || match(project) {
			out = append(out, project)
		}
		return false, nil
	})
	return out, err
}

// collectInitiatives gathers every initiative matching the predicate, in id order.
func (q queryServer) collectInitiatives(ctx context.Context, match func(types.Initiative) bool) ([]types.Initiative, error) {
	var out []types.Initiative
	err := q.k.Initiative.Walk(ctx, nil, func(_ uint64, initiative types.Initiative) (bool, error) {
		if match == nil || match(initiative) {
			out = append(out, initiative)
		}
		return false, nil
	})
	return out, err
}

func cmpUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// nil math pointers (fields absent on old state) compare as zero.
func cmpMaybeInt(a, b *math.Int) int {
	av, bv := math.ZeroInt(), math.ZeroInt()
	if a != nil && !a.IsNil() {
		av = *a
	}
	if b != nil && !b.IsNil() {
		bv = *b
	}
	switch {
	case av.LT(bv):
		return -1
	case av.GT(bv):
		return 1
	default:
		return 0
	}
}

// sortProjects orders the slice by sortBy ("id", "name", "budget", "status"),
// reversed when reverse is set. Ties fall back to id in the same direction so
// pages stay deterministic.
func sortProjects(list []types.Project, sortBy string, reverse bool) error {
	dir := 1
	if reverse {
		dir = -1
	}
	byID := func(a, b types.Project) int { return dir * cmpUint64(a.Id, b.Id) }
	var cmp func(a, b types.Project) int
	switch sortBy {
	case "", "id":
		cmp = byID
	case "name":
		cmp = func(a, b types.Project) int {
			if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "budget":
		cmp = func(a, b types.Project) int {
			if c := cmpMaybeInt(a.ApprovedBudget, b.ApprovedBudget); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "status":
		cmp = func(a, b types.Project) int {
			if c := cmpUint64(uint64(a.Status), uint64(b.Status)); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	default:
		return status.Errorf(codes.InvalidArgument, "unknown sort_by %q (want id, name, budget or status)", sortBy)
	}
	slices.SortStableFunc(list, cmp)
	return nil
}

// initiativeConvictionRatio returns current/required and whether a ratio
// exists at all (required must be positive).
func initiativeConvictionRatio(ini types.Initiative) (math.LegacyDec, bool) {
	if ini.RequiredConviction == nil || ini.RequiredConviction.IsNil() || !ini.RequiredConviction.IsPositive() {
		return math.LegacyZeroDec(), false
	}
	cur := math.LegacyZeroDec()
	if ini.CurrentConviction != nil && !ini.CurrentConviction.IsNil() {
		cur = *ini.CurrentConviction
	}
	return cur.Quo(*ini.RequiredConviction), true
}

// sortInitiatives orders the slice by sortBy ("id", "title", "status",
// "budget", "tier", "conviction"), reversed when reverse is set. Initiatives
// without a required conviction have no completion ratio, so under
// "conviction" they sort after every initiative that has one regardless of
// direction. Ties fall back to id in the same direction.
func sortInitiatives(list []types.Initiative, sortBy string, reverse bool) error {
	dir := 1
	if reverse {
		dir = -1
	}
	byID := func(a, b types.Initiative) int { return dir * cmpUint64(a.Id, b.Id) }
	var cmp func(a, b types.Initiative) int
	switch sortBy {
	case "", "id":
		cmp = byID
	case "title":
		cmp = func(a, b types.Initiative) int {
			if c := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "status":
		cmp = func(a, b types.Initiative) int {
			if c := cmpUint64(uint64(a.Status), uint64(b.Status)); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "budget":
		cmp = func(a, b types.Initiative) int {
			if c := cmpMaybeInt(a.Budget, b.Budget); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "tier":
		cmp = func(a, b types.Initiative) int {
			if c := cmpUint64(uint64(a.Tier), uint64(b.Tier)); c != 0 {
				return dir * c
			}
			return byID(a, b)
		}
	case "conviction":
		cmp = func(a, b types.Initiative) int {
			ra, aOK := initiativeConvictionRatio(a)
			rb, bOK := initiativeConvictionRatio(b)
			if aOK != bOK {
				if aOK {
					return -1
				}
				return 1
			}
			if aOK {
				if ra.LT(rb) {
					return dir * -1
				}
				if ra.GT(rb) {
					return dir
				}
			}
			return byID(a, b)
		}
	default:
		return status.Errorf(codes.InvalidArgument, "unknown sort_by %q (want id, title, status, budget, tier or conviction)", sortBy)
	}
	slices.SortStableFunc(list, cmp)
	return nil
}

// paginateSorted slices one page out of an already-sorted list. The offset
// comes from pagination.offset or, for "Load more" flows that echo next_key
// back, from pagination.key holding the offset as a decimal string.
func paginateSorted[T any](list []T, page *query.PageRequest) ([]T, *query.PageResponse, error) {
	offset := uint64(0)
	limit := uint64(defaultSortedPageLimit)
	if page != nil {
		if len(page.Key) > 0 {
			parsed, err := strconv.ParseUint(string(page.Key), 10, 64)
			if err != nil {
				return nil, nil, status.Error(codes.InvalidArgument, "pagination key of a sorted query must be a decimal offset")
			}
			offset = parsed
		} else {
			offset = page.Offset
		}
		if page.Limit > 0 {
			limit = page.Limit
		}
	}
	total := uint64(len(list))
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	res := &query.PageResponse{Total: total}
	if end < total {
		res.NextKey = []byte(strconv.FormatUint(end, 10))
	}
	return list[offset:end], res, nil
}

package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"

	forumtypes "sparkdream/x/forum/types"
	"sparkdream/x/rep/types"
)

// Tag budgets are SPARK pools a group policy escrows for awards to forum
// posts carrying the tag. Creating, pausing and withdrawing one is signed
// by the policy (an x/commons proposal); topping up and awarding by any
// member of the group.

// forumPosts returns the ACTIVE forum posts that carry at least one tag.
func (e *simEnv) forumPosts() []forumtypes.Post {
	var out []forumtypes.Post
	var next []byte
	for page := 0; page < 10; page++ {
		res, err := e.forumQuery.ListPost(e.ctx, &forumtypes.QueryAllPostRequest{
			Pagination: &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			break
		}
		for _, p := range res.Post {
			if p.Status == forumtypes.PostStatus_POST_STATUS_ACTIVE && len(p.Tags) > 0 {
				out = append(out, p)
			}
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		next = res.Pagination.NextKey
	}
	return out
}

// budgetTag picks a tag for a new budget of policy: one carried by a forum
// post when there is one (so awards are possible), else any registered tag,
// never one policy already holds an active budget for.
func (e *simEnv) budgetTag(policy string) (string, error) {
	held := map[string]bool{}
	bs, err := e.tagBudgets(func(b types.TagBudget) bool { return b.GroupAccount == policy && b.Active })
	if err != nil {
		return "", err
	}
	for _, b := range bs {
		held[b.Tag] = true
	}
	var pool []string
	for _, p := range e.forumPosts() {
		for _, t := range p.Tags {
			if !held[t] {
				pool = append(pool, t)
			}
		}
	}
	if len(pool) == 0 {
		tags, err := e.tags()
		if err != nil {
			return "", err
		}
		for _, t := range tags {
			if !held[t] {
				pool = append(pool, t)
			}
		}
	}
	t, _ := pick(e.r, pool)
	return t, nil
}

// poolBalance parses a budget's pool balance.
func poolBalance(b types.TagBudget) math.Int {
	v, ok := math.NewIntFromString(b.PoolBalance)
	if !ok {
		return math.ZeroInt()
	}
	return v
}

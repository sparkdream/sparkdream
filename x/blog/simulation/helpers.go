package simulation

import (
	"fmt"
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// ─── find helpers ────────────────────────────────────────────────────────────

// findActivePost returns a random active post, or nil if none exist.
func findActivePost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64) {
	count := k.GetPostCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var posts []types.Post
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		p, found := k.GetPost(ctx, i)
		if found && p.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, p)
			ids = append(ids, i)
		}
	}
	if len(posts) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(posts))
	return &posts[idx], ids[idx]
}

// findHiddenPost returns a random hidden post, or nil if none exist.
func findHiddenPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64) {
	count := k.GetPostCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var posts []types.Post
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		p, found := k.GetPost(ctx, i)
		if found && p.Status == types.PostStatus_POST_STATUS_HIDDEN {
			posts = append(posts, p)
			ids = append(ids, i)
		}
	}
	if len(posts) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(posts))
	return &posts[idx], ids[idx]
}

// findPostByCreator returns a random active post by the given creator, or nil.
func findPostByCreator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (*types.Post, uint64) {
	count := k.GetPostCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var posts []types.Post
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		p, found := k.GetPost(ctx, i)
		if found && p.Creator == creator && p.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, p)
			ids = append(ids, i)
		}
	}
	if len(posts) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(posts))
	return &posts[idx], ids[idx]
}

// findActiveReply returns a random active reply, or nil if none exist.
func findActiveReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Reply, uint64) {
	count := k.GetReplyCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var replies []types.Reply
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		rp, found := k.GetReply(ctx, i)
		if found && rp.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE {
			replies = append(replies, rp)
			ids = append(ids, i)
		}
	}
	if len(replies) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(replies))
	return &replies[idx], ids[idx]
}

// findHiddenReplyOnPost returns a random hidden reply on the given post, or nil.
func findHiddenReplyOnPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64) (*types.Reply, uint64) {
	count := k.GetReplyCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var replies []types.Reply
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		rp, found := k.GetReply(ctx, i)
		if found && rp.PostId == postId && rp.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
			replies = append(replies, rp)
			ids = append(ids, i)
		}
	}
	if len(replies) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(replies))
	return &replies[idx], ids[idx]
}

// findReplyOnPost returns a random active reply on the given post, or nil.
func findReplyOnPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64) (*types.Reply, uint64) {
	count := k.GetReplyCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var replies []types.Reply
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		rp, found := k.GetReply(ctx, i)
		if found && rp.PostId == postId && rp.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE {
			replies = append(replies, rp)
			ids = append(ids, i)
		}
	}
	if len(replies) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(replies))
	return &replies[idx], ids[idx]
}

// findReplyByCreator returns a random active reply by the given creator, or nil.
func findReplyByCreator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (*types.Reply, uint64) {
	count := k.GetReplyCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var replies []types.Reply
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		rp, found := k.GetReply(ctx, i)
		if found && rp.Creator == creator && rp.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE {
			replies = append(replies, rp)
			ids = append(ids, i)
		}
	}
	if len(replies) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(replies))
	return &replies[idx], ids[idx]
}

// findEphemeralPost returns a random active post that has ExpiresAt > 0 and is not pinned.
func findEphemeralPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64) {
	count := k.GetPostCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var posts []types.Post
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		p, found := k.GetPost(ctx, i)
		if found && p.Status == types.PostStatus_POST_STATUS_ACTIVE && p.ExpiresAt > 0 && p.PinnedBy == "" {
			posts = append(posts, p)
			ids = append(ids, i)
		}
	}
	if len(posts) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(posts))
	return &posts[idx], ids[idx]
}

// findEphemeralReplyOnPost returns a random active ephemeral reply on the given post, or nil.
func findEphemeralReplyOnPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64) (*types.Reply, uint64) {
	count := k.GetReplyCount(ctx)
	if count == 0 {
		return nil, 0
	}
	var replies []types.Reply
	var ids []uint64
	for i := uint64(0); i < count; i++ {
		rp, found := k.GetReply(ctx, i)
		if found && rp.PostId == postId && rp.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE && rp.ExpiresAt > 0 && rp.PinnedBy == "" {
			replies = append(replies, rp)
			ids = append(ids, i)
		}
	}
	if len(replies) == 0 {
		return nil, 0
	}
	idx := r.Intn(len(replies))
	return &replies[idx], ids[idx]
}

// ─── get-or-create helpers ──────────────────────────────────────────────────

// getOrCreateActivePost returns an existing active post owned by creator, or creates one.
func getOrCreateActivePost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (uint64, error) {
	p, id := findPostByCreator(r, ctx, k, creator)
	if p != nil {
		return id, nil
	}
	// Create new active post
	post := types.Post{
		Title:          randomTitle(r),
		Body:           randomBody(r),
		Creator:        creator,
		RepliesEnabled: true,
		CreatedAt:      ctx.BlockTime().Unix(),
		Status:         types.PostStatus_POST_STATUS_ACTIVE,
	}
	return k.AppendPost(ctx, post), nil
}

// getOrCreateAnyActivePost returns any existing active post, or creates one owned by creator.
func getOrCreateAnyActivePost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (uint64, error) {
	p, id := findActivePost(r, ctx, k)
	if p != nil {
		return id, nil
	}
	return getOrCreateActivePost(r, ctx, k, creator)
}

// getOrCreateReplyOnPost returns an existing active reply on the post, or creates one.
func getOrCreateReplyOnPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64, creator string) (uint64, error) {
	rp, id := findReplyOnPost(r, ctx, k, postId)
	if rp != nil {
		return id, nil
	}
	// Create new reply on this post
	reply := types.Reply{
		PostId:    postId,
		Creator:   creator,
		Body:      randomBody(r),
		CreatedAt: ctx.BlockTime().Unix(),
		Status:    types.ReplyStatus_REPLY_STATUS_ACTIVE,
		Depth:     1,
	}
	replyID := k.AppendReply(ctx, reply)

	// Increment post reply count
	post, found := k.GetPost(ctx, postId)
	if found {
		post.ReplyCount++
		k.SetPost(ctx, post)
	}
	return replyID, nil
}

// getOrCreateHiddenPost returns an existing hidden post, or creates and hides one.
func getOrCreateHiddenPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, hider string) (uint64, error) {
	p, id := findHiddenPost(r, ctx, k)
	if p != nil {
		return id, nil
	}
	// Create active post and hide it
	postID, err := getOrCreateActivePost(r, ctx, k, hider)
	if err != nil {
		return 0, err
	}
	post, found := k.GetPost(ctx, postID)
	if !found {
		return 0, fmt.Errorf("post %d not found after creation", postID)
	}
	post.Status = types.PostStatus_POST_STATUS_HIDDEN
	post.HiddenBy = hider
	post.HiddenAt = ctx.BlockTime().Unix()
	k.SetPost(ctx, post)
	return postID, nil
}

// getOrCreateHiddenReply returns an existing hidden reply on the given post, or creates and hides one.
func getOrCreateHiddenReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64, hider string) (uint64, error) {
	rp, id := findHiddenReplyOnPost(r, ctx, k, postId)
	if rp != nil {
		return id, nil
	}
	// Create active reply and hide it
	replyID, err := getOrCreateReplyOnPost(r, ctx, k, postId, hider)
	if err != nil {
		return 0, err
	}
	reply, found := k.GetReply(ctx, replyID)
	if !found {
		return 0, fmt.Errorf("reply %d not found after creation", replyID)
	}
	reply.Status = types.ReplyStatus_REPLY_STATUS_HIDDEN
	reply.HiddenBy = hider
	reply.HiddenAt = ctx.BlockTime().Unix()
	k.SetReply(ctx, reply)

	// Decrement post reply count (hidden replies don't count)
	post, found := k.GetPost(ctx, postId)
	if found && post.ReplyCount > 0 {
		post.ReplyCount--
		k.SetPost(ctx, post)
	}
	return replyID, nil
}

// getOrCreateEphemeralPost returns an existing ephemeral post, or creates one.
func getOrCreateEphemeralPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (uint64, error) {
	p, id := findEphemeralPost(r, ctx, k)
	if p != nil {
		return id, nil
	}
	// Create post with expiry
	expiresAt := ctx.BlockTime().Unix() + 86400
	post := types.Post{
		Title:          randomTitle(r),
		Body:           randomBody(r),
		Creator:        creator,
		RepliesEnabled: true,
		CreatedAt:      ctx.BlockTime().Unix(),
		Status:         types.PostStatus_POST_STATUS_ACTIVE,
		ExpiresAt:      expiresAt,
	}
	postID := k.AppendPost(ctx, post)
	k.AddToExpiryIndex(ctx, expiresAt, "post", postID)
	return postID, nil
}

// getOrCreateEphemeralReply returns an existing ephemeral reply, or creates one on the given post.
func getOrCreateEphemeralReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64, creator string) (uint64, error) {
	rp, id := findEphemeralReplyOnPost(r, ctx, k, postId)
	if rp != nil {
		return id, nil
	}
	// Create reply with expiry
	expiresAt := ctx.BlockTime().Unix() + 86400
	reply := types.Reply{
		PostId:    postId,
		Creator:   creator,
		Body:      randomBody(r),
		CreatedAt: ctx.BlockTime().Unix(),
		Status:    types.ReplyStatus_REPLY_STATUS_ACTIVE,
		ExpiresAt: expiresAt,
		Depth:     1,
	}
	replyID := k.AppendReply(ctx, reply)
	k.AddToExpiryIndex(ctx, expiresAt, "reply", replyID)

	// Increment post reply count
	post, found := k.GetPost(ctx, postId)
	if found {
		post.ReplyCount++
		k.SetPost(ctx, post)
	}
	return replyID, nil
}

// getOrCreateReaction ensures a reaction exists by creator on the given target.
func getOrCreateReaction(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, postId uint64, replyId uint64, creator string) error {
	_, found := k.GetReaction(ctx, postId, replyId, creator)
	if found {
		return nil
	}
	rt := randomReactionType(r)
	reaction := types.Reaction{
		Creator:      creator,
		ReactionType: rt,
		PostId:       postId,
		ReplyId:      replyId,
	}
	k.SetReaction(ctx, reaction)

	// Increment reaction counts
	counts := k.GetReactionCounts(ctx, postId, replyId)
	incrementReactionCount(&counts, rt)
	k.SetReactionCounts(ctx, postId, replyId, counts)
	return nil
}

// ─── reaction count helpers ─────────────────────────────────────────────────

func incrementReactionCount(counts *types.ReactionCounts, rt types.ReactionType) {
	switch rt {
	case types.ReactionType_REACTION_TYPE_LIKE:
		counts.LikeCount++
	case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
		counts.InsightfulCount++
	case types.ReactionType_REACTION_TYPE_DISAGREE:
		counts.DisagreeCount++
	case types.ReactionType_REACTION_TYPE_FUNNY:
		counts.FunnyCount++
	}
}

func decrementReactionCount(counts *types.ReactionCounts, rt types.ReactionType) {
	switch rt {
	case types.ReactionType_REACTION_TYPE_LIKE:
		if counts.LikeCount > 0 {
			counts.LikeCount--
		}
	case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
		if counts.InsightfulCount > 0 {
			counts.InsightfulCount--
		}
	case types.ReactionType_REACTION_TYPE_DISAGREE:
		if counts.DisagreeCount > 0 {
			counts.DisagreeCount--
		}
	case types.ReactionType_REACTION_TYPE_FUNNY:
		if counts.FunnyCount > 0 {
			counts.FunnyCount--
		}
	}
}

// ─── utility helpers ────────────────────────────────────────────────────────

func randomReactionType(r *rand.Rand) types.ReactionType {
	choices := []types.ReactionType{
		types.ReactionType_REACTION_TYPE_LIKE,
		types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		types.ReactionType_REACTION_TYPE_DISAGREE,
		types.ReactionType_REACTION_TYPE_FUNNY,
	}
	return choices[r.Intn(len(choices))]
}

func randomBody(r *rand.Rand) string {
	bodies := []string{
		"Simulation blog body content.",
		"Testing the blog module with random content.",
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		"This was created during simulation testing.",
		"Sample content for blog simulation.",
	}
	return bodies[r.Intn(len(bodies))]
}

func randomTitle(r *rand.Rand) string {
	titles := []string{
		"Simulation Post",
		"Test Blog Entry",
		"Random Thoughts",
		"Sample Article",
		"Quick Update",
	}
	return titles[r.Intn(len(titles))]
}

func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Posts:          []Post{},
		PostCount:      0,
		Replies:        []Reply{},
		ReplyCount:     0,
		Reactions:      []Reaction{},
		ReactionCounts: []GenesisReactionCounts{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Build lookup maps for cross-referencing
	postIDSeen := make(map[uint64]bool, len(gs.Posts))
	postMap := make(map[uint64]*Post, len(gs.Posts))
	replyIDSeen := make(map[uint64]bool, len(gs.Replies))
	replyMap := make(map[uint64]*Reply, len(gs.Replies))

	// --- Post validation ---
	for i := range gs.Posts {
		post := &gs.Posts[i]

		if post.Id >= gs.PostCount {
			return fmt.Errorf("post ID %d is >= post_count %d", post.Id, gs.PostCount)
		}
		if postIDSeen[post.Id] {
			return fmt.Errorf("duplicate post ID %d", post.Id)
		}
		postIDSeen[post.Id] = true
		postMap[post.Id] = post

		// Status must not be UNSPECIFIED
		if post.Status == PostStatus_POST_STATUS_UNSPECIFIED {
			return fmt.Errorf("post %d has UNSPECIFIED status", post.Id)
		}

		// Pinned content must have expires_at == 0
		if post.PinnedBy != "" && post.ExpiresAt != 0 {
			return fmt.Errorf("post %d is pinned but has expires_at %d (must be 0)", post.Id, post.ExpiresAt)
		}
	}

	// --- Reply validation ---
	for i := range gs.Replies {
		reply := &gs.Replies[i]

		if reply.Id >= gs.ReplyCount {
			return fmt.Errorf("reply ID %d is >= reply_count %d", reply.Id, gs.ReplyCount)
		}
		if replyIDSeen[reply.Id] {
			return fmt.Errorf("duplicate reply ID %d", reply.Id)
		}
		replyIDSeen[reply.Id] = true
		replyMap[reply.Id] = reply

		// Status must not be UNSPECIFIED
		if reply.Status == ReplyStatus_REPLY_STATUS_UNSPECIFIED {
			return fmt.Errorf("reply %d has UNSPECIFIED status", reply.Id)
		}

		// Reply must reference an existing post
		if _, ok := postMap[reply.PostId]; !ok {
			return fmt.Errorf("reply %d references non-existent post %d", reply.Id, reply.PostId)
		}

		// Parent reply must exist (or 0 for top-level)
		if reply.ParentReplyId != 0 {
			if _, ok := replyMap[reply.ParentReplyId]; !ok {
				return fmt.Errorf("reply %d references non-existent parent reply %d", reply.Id, reply.ParentReplyId)
			}
		}

		// Pinned content must have expires_at == 0
		if reply.PinnedBy != "" && reply.ExpiresAt != 0 {
			return fmt.Errorf("reply %d is pinned but has expires_at %d (must be 0)", reply.Id, reply.ExpiresAt)
		}
	}

	// --- Reply depth consistency ---
	for _, reply := range gs.Replies {
		if reply.ParentReplyId == 0 {
			if reply.Depth != 0 {
				return fmt.Errorf("reply %d is top-level but has depth %d (expected 0)", reply.Id, reply.Depth)
			}
		} else {
			parent, ok := replyMap[reply.ParentReplyId]
			if ok && reply.Depth != parent.Depth+1 {
				return fmt.Errorf("reply %d has depth %d but parent reply %d has depth %d (expected %d)",
					reply.Id, reply.Depth, reply.ParentReplyId, parent.Depth, parent.Depth+1)
			}
		}
	}

	// --- Reply count consistency ---
	activeReplyCounts := make(map[uint64]uint64) // post_id -> count of ACTIVE replies
	for _, reply := range gs.Replies {
		if reply.Status == ReplyStatus_REPLY_STATUS_ACTIVE {
			activeReplyCounts[reply.PostId]++
		}
	}
	for _, post := range gs.Posts {
		expected := activeReplyCounts[post.Id]
		if post.ReplyCount != expected {
			return fmt.Errorf("post %d has reply_count %d but %d active replies exist",
				post.Id, post.ReplyCount, expected)
		}
	}

	// --- Reaction validation ---
	type reactionKey struct {
		PostID  uint64
		ReplyID uint64
		Creator string
	}
	reactionSeen := make(map[reactionKey]bool, len(gs.Reactions))
	// Track counts for consistency check
	type countKey struct {
		PostID  uint64
		ReplyID uint64
	}
	recomputedCounts := make(map[countKey]map[ReactionType]uint64)

	for _, reaction := range gs.Reactions {
		// Check for duplicate reactions
		rk := reactionKey{PostID: reaction.PostId, ReplyID: reaction.ReplyId, Creator: reaction.Creator}
		if reactionSeen[rk] {
			return fmt.Errorf("duplicate reaction: post=%d reply=%d creator=%s",
				reaction.PostId, reaction.ReplyId, reaction.Creator)
		}
		reactionSeen[rk] = true

		// Reaction target must reference existing post
		if _, ok := postMap[reaction.PostId]; !ok {
			return fmt.Errorf("reaction references non-existent post %d", reaction.PostId)
		}
		// If targeting a reply, reply must exist
		if reaction.ReplyId != 0 {
			if _, ok := replyMap[reaction.ReplyId]; !ok {
				return fmt.Errorf("reaction references non-existent reply %d", reaction.ReplyId)
			}
		}

		// Track for count recomputation
		ck := countKey{PostID: reaction.PostId, ReplyID: reaction.ReplyId}
		if recomputedCounts[ck] == nil {
			recomputedCounts[ck] = make(map[ReactionType]uint64)
		}
		recomputedCounts[ck][reaction.ReactionType]++
	}

	// --- Reaction counts consistency ---
	for _, rc := range gs.ReactionCounts {
		if rc.Counts == nil {
			continue
		}
		ck := countKey{PostID: rc.PostId, ReplyID: rc.ReplyId}
		expected := recomputedCounts[ck]
		if expected == nil {
			expected = make(map[ReactionType]uint64)
		}

		// Check each named count field
		checks := []struct {
			name   string
			stored uint64
			rtype  ReactionType
		}{
			{"like", rc.Counts.LikeCount, ReactionType_REACTION_TYPE_LIKE},
			{"insightful", rc.Counts.InsightfulCount, ReactionType_REACTION_TYPE_INSIGHTFUL},
			{"disagree", rc.Counts.DisagreeCount, ReactionType_REACTION_TYPE_DISAGREE},
			{"funny", rc.Counts.FunnyCount, ReactionType_REACTION_TYPE_FUNNY},
		}
		for _, c := range checks {
			exp := expected[c.rtype]
			if c.stored != exp {
				return fmt.Errorf("reaction count mismatch for post=%d reply=%d %s: stored=%d computed=%d",
					rc.PostId, rc.ReplyId, c.name, c.stored, exp)
			}
		}
	}

	return nil
}

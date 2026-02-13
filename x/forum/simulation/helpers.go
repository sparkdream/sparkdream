package simulation

import (
	"fmt"
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// findCategory returns a random category from state
func findCategory(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Category, uint64, error) {
	var categories []struct {
		id       uint64
		category types.Category
	}
	err := k.Category.Walk(ctx, nil, func(id uint64, category types.Category) (bool, error) {
		categories = append(categories, struct {
			id       uint64
			category types.Category
		}{id, category})
		return false, nil
	})
	if err != nil || len(categories) == 0 {
		return nil, 0, err
	}
	selected := categories[r.Intn(len(categories))]
	return &selected.category, selected.id, nil
}

// findPost returns a random post from state
func findPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		posts = append(posts, struct {
			id   uint64
			post types.Post
		}{id, post})
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findPostWithStatus returns a random post with specific status
func findPostWithStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.PostStatus) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.Status == status {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findRootPost returns a random root post (thread)
func findRootPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findUnlockedRootPost returns a random unlocked root post
func findUnlockedRootPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE && !post.Locked {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findLockedRootPost returns a random locked root post
func findLockedRootPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.ParentId == 0 && post.Locked {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findPinnedPost returns a random pinned post
func findPinnedPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.Pinned {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findHiddenPost returns a random hidden post
func findHiddenPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findPostByAuthor returns a random post by the given author
func findPostByAuthor(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.Author == author && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findReplyInThread returns a reply in the given thread
func findReplyInThread(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, threadId uint64) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.RootId == threadId && post.ParentId != 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findBounty returns a random bounty from state
func findBounty(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.BountyStatus) (*types.Bounty, uint64, error) {
	var bounties []struct {
		id     uint64
		bounty types.Bounty
	}
	err := k.Bounty.Walk(ctx, nil, func(id uint64, bounty types.Bounty) (bool, error) {
		if bounty.Status == status {
			bounties = append(bounties, struct {
				id     uint64
				bounty types.Bounty
			}{id, bounty})
		}
		return false, nil
	})
	if err != nil || len(bounties) == 0 {
		return nil, 0, err
	}
	selected := bounties[r.Intn(len(bounties))]
	return &selected.bounty, selected.id, nil
}

// findBountyByCreator returns a random bounty created by the given user
func findBountyByCreator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (*types.Bounty, uint64, error) {
	var bounties []struct {
		id     uint64
		bounty types.Bounty
	}
	err := k.Bounty.Walk(ctx, nil, func(id uint64, bounty types.Bounty) (bool, error) {
		if bounty.Creator == creator && bounty.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			bounties = append(bounties, struct {
				id     uint64
				bounty types.Bounty
			}{id, bounty})
		}
		return false, nil
	})
	if err != nil || len(bounties) == 0 {
		return nil, 0, err
	}
	selected := bounties[r.Intn(len(bounties))]
	return &selected.bounty, selected.id, nil
}

// findTag returns a random tag from state
func findTag(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Tag, string, error) {
	var tags []struct {
		name string
		tag  types.Tag
	}
	err := k.Tag.Walk(ctx, nil, func(name string, tag types.Tag) (bool, error) {
		tags = append(tags, struct {
			name string
			tag  types.Tag
		}{name, tag})
		return false, nil
	})
	if err != nil || len(tags) == 0 {
		return nil, "", err
	}
	selected := tags[r.Intn(len(tags))]
	return &selected.tag, selected.name, nil
}

// findTagBudget returns a random tag budget from state
func findTagBudget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, active bool) (*types.TagBudget, uint64, error) {
	var budgets []struct {
		id     uint64
		budget types.TagBudget
	}
	err := k.TagBudget.Walk(ctx, nil, func(id uint64, budget types.TagBudget) (bool, error) {
		if budget.Active == active {
			budgets = append(budgets, struct {
				id     uint64
				budget types.TagBudget
			}{id, budget})
		}
		return false, nil
	})
	if err != nil || len(budgets) == 0 {
		return nil, 0, err
	}
	selected := budgets[r.Intn(len(budgets))]
	return &selected.budget, selected.id, nil
}

// findTagReport returns a random tag report
func findTagReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.TagReport, string, error) {
	var reports []struct {
		tagName string
		report  types.TagReport
	}
	err := k.TagReport.Walk(ctx, nil, func(tagName string, report types.TagReport) (bool, error) {
		reports = append(reports, struct {
			tagName string
			report  types.TagReport
		}{tagName, report})
		return false, nil
	})
	if err != nil || len(reports) == 0 {
		return nil, "", err
	}
	selected := reports[r.Intn(len(reports))]
	return &selected.report, selected.tagName, nil
}

// findMemberReport returns a random member report
func findMemberReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.MemberReportStatus) (*types.MemberReport, string, error) {
	var reports []struct {
		member string
		report types.MemberReport
	}
	err := k.MemberReport.Walk(ctx, nil, func(member string, report types.MemberReport) (bool, error) {
		if report.Status == status {
			reports = append(reports, struct {
				member string
				report types.MemberReport
			}{member, report})
		}
		return false, nil
	})
	if err != nil || len(reports) == 0 {
		return nil, "", err
	}
	selected := reports[r.Intn(len(reports))]
	return &selected.report, selected.member, nil
}

// findArchivedRootPost returns a random root post with ARCHIVED status
func findArchivedRootPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ARCHIVED {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findThreadFollow returns a random thread follow
func findThreadFollow(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, follower string) (*types.ThreadFollow, string, error) {
	var follows []struct {
		key    string
		follow types.ThreadFollow
	}
	err := k.ThreadFollow.Walk(ctx, nil, func(key string, follow types.ThreadFollow) (bool, error) {
		if follow.Follower == follower {
			follows = append(follows, struct {
				key    string
				follow types.ThreadFollow
			}{key, follow})
		}
		return false, nil
	})
	if err != nil || len(follows) == 0 {
		return nil, "", err
	}
	selected := follows[r.Intn(len(follows))]
	return &selected.follow, selected.key, nil
}

// findGovActionAppeal returns a random pending appeal
func findGovActionAppeal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.GovAppealStatus) (*types.GovActionAppeal, uint64, error) {
	var appeals []struct {
		id     uint64
		appeal types.GovActionAppeal
	}
	err := k.GovActionAppeal.Walk(ctx, nil, func(id uint64, appeal types.GovActionAppeal) (bool, error) {
		if appeal.Status == status {
			appeals = append(appeals, struct {
				id     uint64
				appeal types.GovActionAppeal
			}{id, appeal})
		}
		return false, nil
	})
	if err != nil || len(appeals) == 0 {
		return nil, 0, err
	}
	selected := appeals[r.Intn(len(appeals))]
	return &selected.appeal, selected.id, nil
}

// getOrCreateCategory returns an existing category or creates one
func getOrCreateCategory(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (uint64, error) {
	// Try to find existing category
	_, categoryID, err := findCategory(r, ctx, k)
	if err == nil && categoryID != 0 {
		return categoryID, nil
	}

	// Create new category
	categoryID, err = k.CategorySeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	category := types.Category{
		CategoryId:       categoryID,
		Title:            fmt.Sprintf("Category-%d", r.Intn(10000)),
		Description:      "Simulation generated category",
		MembersOnlyWrite: false, // Allow all writes in simulation since no members exist
		AdminOnlyWrite:   false,
	}

	return categoryID, k.Category.Set(ctx, categoryID, category)
}

// getOrCreatePost returns an existing post or creates one
func getOrCreatePost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing post by author
	post, postID, err := findPostByAuthor(r, ctx, k, author)
	if err == nil && post != nil {
		return postID, nil
	}

	// Need to create a post, first ensure we have a category
	categoryID, err := getOrCreateCategory(r, ctx, k)
	if err != nil {
		return 0, err
	}

	// Create new post
	postID, err = k.PostSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	newPost := types.Post{
		PostId:     postID,
		CategoryId: categoryID,
		RootId:     postID, // Root post
		ParentId:   0,
		Author:     author,
		Content:    fmt.Sprintf("Simulation post content %d", r.Intn(10000)),
		CreatedAt:  ctx.BlockTime().Unix(),
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
	}

	return postID, k.Post.Set(ctx, postID, newPost)
}

// getOrCreateRootPostByAuthor returns an existing root post by specific author or creates one
func getOrCreateRootPostByAuthor(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing root post by this author
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.Author == author && post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err == nil && len(posts) > 0 {
		selected := posts[r.Intn(len(posts))]
		return selected.id, nil
	}

	// Need to create a root post
	categoryID, err := getOrCreateCategory(r, ctx, k)
	if err != nil {
		return 0, err
	}

	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	newPost := types.Post{
		PostId:     postID,
		CategoryId: categoryID,
		RootId:     postID, // Root post
		ParentId:   0,
		Author:     author,
		Content:    fmt.Sprintf("Simulation root post content %d", r.Intn(10000)),
		CreatedAt:  ctx.BlockTime().Unix(),
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
	}

	return postID, k.Post.Set(ctx, postID, newPost)
}

// getOrCreateRootPost returns an existing root post or creates one
func getOrCreateRootPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing root post
	post, postID, err := findRootPost(r, ctx, k)
	if err == nil && post != nil {
		return postID, nil
	}

	// Create one
	return getOrCreatePost(r, ctx, k, author)
}

// getOrCreateReply creates a reply to a root post
func getOrCreateReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, threadId uint64) (uint64, error) {
	// Try to find existing reply in thread
	reply, replyID, err := findReplyInThread(r, ctx, k, threadId)
	if err == nil && reply != nil {
		return replyID, nil
	}

	// Get the thread to find category
	thread, err := k.Post.Get(ctx, threadId)
	if err != nil {
		return 0, err
	}

	// Create new reply
	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	newPost := types.Post{
		PostId:     postID,
		CategoryId: thread.CategoryId,
		RootId:     threadId,
		ParentId:   threadId,
		Author:     thread.Author, // Use thread author for simplicity
		Content:    fmt.Sprintf("Simulation reply content %d", r.Intn(10000)),
		CreatedAt:  ctx.BlockTime().Unix(),
		Status:     types.PostStatus_POST_STATUS_ACTIVE,
	}

	return postID, k.Post.Set(ctx, postID, newPost)
}

// getOrCreateBounty returns an existing active bounty or creates one
func getOrCreateBounty(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (uint64, error) {
	// Try to find existing bounty by creator
	bounty, bountyID, err := findBountyByCreator(r, ctx, k, creator)
	if err == nil && bounty != nil {
		return bountyID, nil
	}

	// Need to create bounty, first ensure we have a root post owned by creator
	threadID, err := getOrCreatePost(r, ctx, k, creator)
	if err != nil {
		return 0, err
	}

	// Create new bounty
	bountyID, err = k.BountySeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockTime().Unix()
	duration := int64(86400 * (r.Intn(30) + 7))  // 7-37 days
	amount := fmt.Sprintf("%d", r.Intn(900)+100) // 100-1000

	newBounty := types.Bounty{
		Id:        bountyID,
		Creator:   creator,
		ThreadId:  threadID,
		Amount:    amount,
		CreatedAt: now,
		ExpiresAt: now + duration,
		Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
	}

	return bountyID, k.Bounty.Set(ctx, bountyID, newBounty)
}

// getOrCreateTag returns an existing tag or creates one
func getOrCreateTag(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (string, error) {
	// Try to find existing tag
	_, tagName, err := findTag(r, ctx, k)
	if err == nil && tagName != "" {
		return tagName, nil
	}

	// Create new tag
	tagName = randomTagName(r)
	now := ctx.BlockTime().Unix()

	tag := types.Tag{
		Name:       tagName,
		UsageCount: 0,
		CreatedAt:  now,
		LastUsedAt: now,
	}

	return tagName, k.Tag.Set(ctx, tagName, tag)
}

// getOrCreateTagBudget returns an existing tag budget or creates one
func getOrCreateTagBudget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, groupAccount string) (uint64, error) {
	// Try to find existing active budget
	budget, budgetID, err := findTagBudget(r, ctx, k, true)
	if err == nil && budget != nil {
		return budgetID, nil
	}

	// Ensure tag exists
	tagName, err := getOrCreateTag(r, ctx, k)
	if err != nil {
		return 0, err
	}

	// Create new budget
	budgetID, err = k.TagBudgetSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	poolBalance := fmt.Sprintf("%d", r.Intn(90000)+10000) // 10k-100k
	now := ctx.BlockTime().Unix()

	newBudget := types.TagBudget{
		Id:           budgetID,
		GroupAccount: groupAccount,
		Tag:          tagName,
		PoolBalance:  poolBalance,
		MembersOnly:  r.Intn(2) == 1,
		CreatedAt:    now,
		Active:       true,
	}

	return budgetID, k.TagBudget.Set(ctx, budgetID, newBudget)
}

// getOrCreateMemberReport creates a member report if none exists
func getOrCreateMemberReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, reportedMember string, reporter string) error {
	// Check if report exists
	_, err := k.MemberReport.Get(ctx, reportedMember)
	if err == nil {
		return nil // Report exists
	}

	// Create report
	now := ctx.BlockTime().Unix()
	report := types.MemberReport{
		Member:    reportedMember,
		Reason:    "Simulation report",
		Status:    types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		CreatedAt: now,
		Reporters: []string{reporter},
		TotalBond: fmt.Sprintf("%d", r.Intn(900)+100),
	}

	return k.MemberReport.Set(ctx, reportedMember, report)
}

// getOrCreateTagReport creates a tag report if none exists
func getOrCreateTagReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, tagName string, reporter string) error {
	// Check if report exists
	_, err := k.TagReport.Get(ctx, tagName)
	if err == nil {
		return nil // Report exists
	}

	// Ensure tag exists
	_, err = k.Tag.Get(ctx, tagName)
	if err != nil {
		// Create tag
		now := ctx.BlockTime().Unix()
		tag := types.Tag{
			Name:       tagName,
			UsageCount: 0,
			CreatedAt:  now,
			LastUsedAt: now,
		}
		if err := k.Tag.Set(ctx, tagName, tag); err != nil {
			return err
		}
	}

	// Create report
	now := ctx.BlockTime().Unix()
	report := types.TagReport{
		TagName:       tagName,
		TotalBond:     fmt.Sprintf("%d", r.Intn(900)+100),
		FirstReportAt: now,
		UnderReview:   false,
		Reporters:     []string{reporter},
	}

	return k.TagReport.Set(ctx, tagName, report)
}

// getOrCreateThreadMetadata gets or creates thread metadata
func getOrCreateThreadMetadata(ctx sdk.Context, k keeper.Keeper, threadId uint64) (*types.ThreadMetadata, error) {
	metadata, err := k.ThreadMetadata.Get(ctx, threadId)
	if err == nil {
		return &metadata, nil
	}

	// Create new metadata
	metadata = types.ThreadMetadata{
		ThreadId:       threadId,
		PinnedReplyIds: []uint64{},
		PinnedRecords:  []*types.PinnedReplyRecord{},
	}

	return &metadata, k.ThreadMetadata.Set(ctx, threadId, metadata)
}

// getOrCreatePostFlag gets or creates post flag data
func getOrCreatePostFlag(ctx sdk.Context, k keeper.Keeper, postId uint64) (*types.PostFlag, error) {
	flag, err := k.PostFlag.Get(ctx, postId)
	if err == nil {
		return &flag, nil
	}

	// Create new flag record
	flag = types.PostFlag{
		PostId:        postId,
		TotalWeight:   "0",
		InReviewQueue: false,
		Flaggers:      []string{},
	}

	return &flag, k.PostFlag.Set(ctx, postId, flag)
}

// randomTagName generates a random tag name
func randomTagName(r *rand.Rand) string {
	tags := []string{"golang", "rust", "python", "javascript", "devops", "testing", "documentation", "design", "frontend", "backend"}
	return tags[r.Intn(len(tags))]
}

// randomContent generates random post content
func randomContent(r *rand.Rand) string {
	contents := []string{
		"This is a simulation generated post content.",
		"Testing the forum module with random content.",
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		"This post was created during simulation testing.",
		"Sample content for forum simulation.",
	}
	return contents[r.Intn(len(contents))]
}

// randomReason generates a random reason string
func randomReason(r *rand.Rand) string {
	reasons := []string{
		"Spam content",
		"Inappropriate content",
		"Off-topic",
		"Low quality",
		"Violation of rules",
	}
	return reasons[r.Intn(len(reasons))]
}

// getAccountForAddress finds a simulation account for the given address
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// getAuthority returns the module authority as string
func getAuthority(k keeper.Keeper) string {
	return k.GetAuthorityString()
}

// findActiveBounty returns a random active bounty
func findActiveBounty(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Bounty, uint64, error) {
	return findBounty(r, ctx, k, types.BountyStatus_BOUNTY_STATUS_ACTIVE)
}

// findPendingTagReport returns a random pending tag report
func findPendingTagReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.TagReport, uint64, error) {
	var reports []struct {
		id     uint64
		report types.TagReport
	}
	var reportID uint64
	err := k.TagReport.Walk(ctx, nil, func(tagName string, report types.TagReport) (bool, error) {
		reportID++
		if !report.UnderReview {
			reports = append(reports, struct {
				id     uint64
				report types.TagReport
			}{reportID, report})
		}
		return false, nil
	})
	if err != nil || len(reports) == 0 {
		return nil, 0, err
	}
	selected := reports[r.Intn(len(reports))]
	return &selected.report, selected.id, nil
}

// findBondedSentinel returns a random bonded sentinel
func findBondedSentinel(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.SentinelActivity, string, error) {
	var sentinels []struct {
		addr     string
		sentinel types.SentinelActivity
	}
	err := k.SentinelActivity.Walk(ctx, nil, func(addr string, sentinel types.SentinelActivity) (bool, error) {
		if sentinel.CurrentBond != "" && sentinel.CurrentBond != "0" {
			sentinels = append(sentinels, struct {
				addr     string
				sentinel types.SentinelActivity
			}{addr, sentinel})
		}
		return false, nil
	})
	if err != nil || len(sentinels) == 0 {
		return nil, "", err
	}
	selected := sentinels[r.Intn(len(sentinels))]
	return &selected.sentinel, selected.addr, nil
}

// findPendingMemberReport returns a random pending member report
func findPendingMemberReport(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.MemberReport, uint64, error) {
	var reports []struct {
		id     uint64
		report types.MemberReport
	}
	var reportID uint64
	err := k.MemberReport.Walk(ctx, nil, func(member string, report types.MemberReport) (bool, error) {
		reportID++
		if report.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
			reports = append(reports, struct {
				id     uint64
				report types.MemberReport
			}{reportID, report})
		}
		return false, nil
	})
	if err != nil || len(reports) == 0 {
		return nil, 0, err
	}
	selected := reports[r.Intn(len(reports))]
	return &selected.report, selected.id, nil
}

// findPinnedReply returns a random pinned reply
func findPinnedReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		// A reply is pinned if it has ParentId != 0 and is pinned
		if post.ParentId != 0 && post.Pinned {
			posts = append(posts, struct {
				id   uint64
				post types.Post
			}{id, post})
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// findPostWithProposedReply returns a random root post (there's no ProposedAcceptedReplyId field on Post)
// This is a placeholder that returns any root post for simulation purposes
func findPostWithProposedReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Post, uint64, error) {
	// Since Post doesn't have ProposedAcceptedReplyId field, return nil to trigger NoOp
	return nil, 0, nil
}

// getOrCreateHiddenPost returns an existing hidden post or creates one by hiding a post
func getOrCreateHiddenPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing hidden post with a hide record
	post, postID, err := findHiddenPost(r, ctx, k)
	if err == nil && post != nil {
		// Check if it has a hide record (sentinel hide)
		_, hideErr := k.HideRecord.Get(ctx, postID)
		if hideErr == nil {
			return postID, nil
		}
	}

	// Create a post and hide it with a sentinel hide record
	postID, err = getOrCreatePost(r, ctx, k, author)
	if err != nil {
		return 0, err
	}

	// Get the post and update its status to hidden
	existingPost, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, err
	}

	existingPost.Status = types.PostStatus_POST_STATUS_HIDDEN
	if err := k.Post.Set(ctx, postID, existingPost); err != nil {
		return 0, err
	}

	// Create a hide record (sentinel hide) so the post can be appealed
	// Set HiddenAt in the past so cooldown has passed
	hideRecord := types.HideRecord{
		PostId:                  postID,
		Sentinel:                author, // Use author as sentinel for simulation
		HiddenAt:                ctx.BlockTime().Unix() - types.DefaultHideAppealCooldown - 1,
		SentinelBondSnapshot:    "1000",
		SentinelBackingSnapshot: "10000",
		CommittedAmount:         "250",
		ReasonCode:              types.ModerationReason_MODERATION_REASON_SPAM,
		ReasonText:              "Simulation test hide",
	}
	return postID, k.HideRecord.Set(ctx, postID, hideRecord)
}

// getOrCreateLockedThread returns an existing locked thread or creates and locks one
func getOrCreateLockedThread(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing locked thread with a lock record
	post, postID, err := findLockedRootPost(r, ctx, k)
	if err == nil && post != nil {
		// Check if it has a lock record
		_, lockErr := k.ThreadLockRecord.Get(ctx, postID)
		if lockErr == nil {
			return postID, nil
		}
	}

	// Create a root post and lock it
	postID, err = getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, err
	}

	// Get the post and set it as locked
	existingPost, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, err
	}

	existingPost.Locked = true
	if err := k.Post.Set(ctx, postID, existingPost); err != nil {
		return 0, err
	}

	// Create a lock record (sentinel lock) so the thread can be appealed
	// Set LockedAt in the past so cooldown has passed
	lockRecord := types.ThreadLockRecord{
		RootId:     postID,
		Sentinel:   author, // Use author as sentinel for simulation
		LockedAt:   ctx.BlockTime().Unix() - types.DefaultLockAppealCooldown - 1,
		LockReason: "Simulation test lock",
	}
	return postID, k.ThreadLockRecord.Set(ctx, postID, lockRecord)
}

// getOrCreateMovedThread returns an existing moved thread or creates and moves one
func getOrCreateMovedThread(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing thread with a move record
	var movedPostID uint64
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		if post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			_, moveErr := k.ThreadMoveRecord.Get(ctx, id)
			if moveErr == nil {
				movedPostID = id
				return true, nil // Stop iteration
			}
		}
		return false, nil
	})
	if err == nil && movedPostID != 0 {
		return movedPostID, nil
	}

	// Create a root post and add a move record
	postID, err := getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, err
	}

	// Get the post to find its category
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, err
	}

	// Create a move record (sentinel move) so the thread can be appealed
	// Set MovedAt in the past so cooldown has passed
	moveRecord := types.ThreadMoveRecord{
		RootId:             postID,
		Sentinel:           author, // Use author as sentinel for simulation
		OriginalCategoryId: post.CategoryId,
		NewCategoryId:      post.CategoryId + 1, // Simulate move to different category
		MovedAt:            ctx.BlockTime().Unix() - types.DefaultMoveAppealCooldown - 1,
		MoveReason:         "Simulation test move",
	}
	return postID, k.ThreadMoveRecord.Set(ctx, postID, moveRecord)
}

// getOrCreatePinnedPost returns an existing pinned post or creates and pins one
func getOrCreatePinnedPost(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing pinned post
	post, postID, err := findPinnedPost(r, ctx, k)
	if err == nil && post != nil {
		return postID, nil
	}

	// Create a root post and pin it
	postID, err = getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, err
	}

	// Get the post and set it as pinned
	existingPost, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, err
	}

	existingPost.Pinned = true
	return postID, k.Post.Set(ctx, postID, existingPost)
}

// getOrCreateThreadFollow returns an existing thread follow or creates one
func getOrCreateThreadFollow(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, follower string) (uint64, error) {
	// Try to find existing follow
	follow, _, err := findThreadFollow(r, ctx, k, follower)
	if err == nil && follow != nil {
		return follow.ThreadId, nil
	}

	// Create a thread and follow it
	threadID, err := getOrCreateRootPost(r, ctx, k, follower)
	if err != nil {
		return 0, err
	}

	// Create follow record - key format must match msg_server: "address:threadId"
	key := fmt.Sprintf("%s:%d", follower, threadID)
	followRecord := types.ThreadFollow{
		ThreadId: threadID,
		Follower: follower,
	}

	return threadID, k.ThreadFollow.Set(ctx, key, followRecord)
}

// getOrCreateBondedSentinel returns an existing bonded sentinel or creates one for the specific address
func getOrCreateBondedSentinel(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, sentinel string) error {
	// Check if this specific sentinel already exists
	existing, err := k.SentinelActivity.Get(ctx, sentinel)
	if err == nil && existing.CurrentBond != "" && existing.CurrentBond != "0" {
		return nil // Already a bonded sentinel
	}

	// Create sentinel activity with bond
	bondAmount := fmt.Sprintf("%d", 100+r.Intn(500))

	activity := types.SentinelActivity{
		Address:            sentinel,
		CurrentBond:        bondAmount,
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		TotalHides:         0,
		UpheldHides:        0,
		OverturnedHides:    0,
		CumulativeRewards:  "0",
		TotalCommittedBond: "0", // No committed bond initially so available bond > 0
	}

	return k.SentinelActivity.Set(ctx, sentinel, activity)
}

// getOrCreatePinnedReply returns an existing pinned reply or creates and pins one
func getOrCreatePinnedReply(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, uint64, error) {
	// Try to find existing pinned reply
	post, postID, err := findPinnedReply(r, ctx, k)
	if err == nil && post != nil {
		return post.RootId, postID, nil
	}

	// Create a thread and a reply, then pin the reply
	threadID, err := getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, 0, err
	}

	replyID, err := getOrCreateReply(r, ctx, k, threadID)
	if err != nil {
		return 0, 0, err
	}

	// Get the reply and set it as pinned
	reply, err := k.Post.Get(ctx, replyID)
	if err != nil {
		return 0, 0, err
	}

	reply.Pinned = true
	if err := k.Post.Set(ctx, replyID, reply); err != nil {
		return 0, 0, err
	}

	return threadID, replyID, nil
}

// getOrCreatePinnedReplyWithMetadata returns an existing pinned reply with metadata or creates one
// This ensures the ThreadMetadata.PinnedRecords contains a PinnedReplyRecord for the pin
func getOrCreatePinnedReplyWithMetadata(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, uint64, error) {
	// Try to find existing pinned reply with proper metadata record
	post, postID, err := findPinnedReply(r, ctx, k)
	if err == nil && post != nil {
		// Check if it has a proper metadata record
		metadata, metaErr := k.ThreadMetadata.Get(ctx, post.RootId)
		if metaErr == nil {
			for _, record := range metadata.PinnedRecords {
				if record.PostId == postID && record.IsSentinelPin && !record.Disputed {
					return post.RootId, postID, nil
				}
			}
		}
	}

	// Create a thread and a reply, then pin the reply with proper metadata
	threadID, err := getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, 0, err
	}

	replyID, err := getOrCreateReply(r, ctx, k, threadID)
	if err != nil {
		return 0, 0, err
	}

	// Get the reply and set it as pinned
	reply, err := k.Post.Get(ctx, replyID)
	if err != nil {
		return 0, 0, err
	}

	reply.Pinned = true
	if err := k.Post.Set(ctx, replyID, reply); err != nil {
		return 0, 0, err
	}

	// Get or create thread metadata with pinned record
	metadata, err := k.ThreadMetadata.Get(ctx, threadID)
	if err != nil {
		// Create new metadata
		metadata = types.ThreadMetadata{
			ThreadId:       threadID,
			PinnedReplyIds: []uint64{replyID},
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
	}

	// Add the pinned record (sentinel pin, not disputed)
	pinnedRecord := &types.PinnedReplyRecord{
		PostId:        replyID,
		PinnedBy:      author, // Sentinel who pinned
		PinnedAt:      ctx.BlockTime().Unix(),
		IsSentinelPin: true, // Sentinel pin (can be disputed)
		Disputed:      false,
	}
	metadata.PinnedRecords = append(metadata.PinnedRecords, pinnedRecord)
	if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
		return 0, 0, err
	}

	return threadID, replyID, nil
}

// findPostWithTag returns a random post that has the specified tag
func findPostWithTag(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, tag string) (*types.Post, uint64, error) {
	var posts []struct {
		id   uint64
		post types.Post
	}
	err := k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
		for _, t := range post.Tags {
			if t == tag {
				posts = append(posts, struct {
					id   uint64
					post types.Post
				}{id, post})
				break
			}
		}
		return false, nil
	})
	if err != nil || len(posts) == 0 {
		return nil, 0, err
	}
	selected := posts[r.Intn(len(posts))]
	return &selected.post, selected.id, nil
}

// getOrCreateArchivedThread returns an existing archived root post or creates one
func getOrCreateArchivedThread(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing archived root post
	post, postID, err := findArchivedRootPost(r, ctx, k)
	if err == nil && post != nil {
		return postID, nil
	}

	// Create a root post and set its status to archived
	postID, err = getOrCreateRootPost(r, ctx, k, author)
	if err != nil {
		return 0, err
	}

	existingPost, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, err
	}

	existingPost.Status = types.PostStatus_POST_STATUS_ARCHIVED
	return postID, k.Post.Set(ctx, postID, existingPost)
}

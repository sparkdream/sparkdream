package forum

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/forum/types"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Shows the parameters of the module",
				},
				{
					RpcMethod: "ListPost",
					Use:       "list-post",
					Short:     "List all post",
				},
				{
					RpcMethod:      "GetPost",
					Use:            "get-post [id]",
					Short:          "Gets a post",
					Alias:          []string{"show-post"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod: "ListCategory",
					Use:       "list-category",
					Short:     "List all category",
				},
				{
					RpcMethod:      "GetCategory",
					Use:            "get-category [id]",
					Short:          "Gets a category",
					Alias:          []string{"show-category"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "category_id"}},
				},
				{
					RpcMethod: "ListUserRateLimit",
					Use:       "list-user-rate-limit",
					Short:     "List all user-rate-limit",
				},
				{
					RpcMethod:      "GetUserRateLimit",
					Use:            "get-user-rate-limit [id]",
					Short:          "Gets a user-rate-limit",
					Alias:          []string{"show-user-rate-limit"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "user_address"}},
				},
				{
					RpcMethod: "ListUserReactionLimit",
					Use:       "list-user-reaction-limit",
					Short:     "List all user-reaction-limit",
				},
				{
					RpcMethod:      "GetUserReactionLimit",
					Use:            "get-user-reaction-limit [id]",
					Short:          "Gets a user-reaction-limit",
					Alias:          []string{"show-user-reaction-limit"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "user_address"}},
				},
				{
					RpcMethod: "ListSentinelActivity",
					Use:       "list-sentinel-activity",
					Short:     "List all sentinel-activity",
				},
				{
					RpcMethod:      "GetSentinelActivity",
					Use:            "get-sentinel-activity [id]",
					Short:          "Gets a sentinel-activity",
					Alias:          []string{"show-sentinel-activity"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListHideRecord",
					Use:       "list-hide-record",
					Short:     "List all hide-record",
				},
				{
					RpcMethod:      "GetHideRecord",
					Use:            "get-hide-record [id]",
					Short:          "Gets a hide-record",
					Alias:          []string{"show-hide-record"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod: "ListThreadLockRecord",
					Use:       "list-thread-lock-record",
					Short:     "List all thread-lock-record",
				},
				{
					RpcMethod:      "GetThreadLockRecord",
					Use:            "get-thread-lock-record [id]",
					Short:          "Gets a thread-lock-record",
					Alias:          []string{"show-thread-lock-record"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod: "ListThreadMoveRecord",
					Use:       "list-thread-move-record",
					Short:     "List all thread-move-record",
				},
				{
					RpcMethod:      "GetThreadMoveRecord",
					Use:            "get-thread-move-record [id]",
					Short:          "Gets a thread-move-record",
					Alias:          []string{"show-thread-move-record"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod: "ListPostFlag",
					Use:       "list-post-flag",
					Short:     "List all post-flag",
				},
				{
					RpcMethod:      "GetPostFlag",
					Use:            "get-post-flag [id]",
					Short:          "Gets a post-flag",
					Alias:          []string{"show-post-flag"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod: "ListBounty",
					Use:       "list-bounty",
					Short:     "List all bounty",
				},
				{
					RpcMethod:      "GetBounty",
					Use:            "get-bounty [id]",
					Short:          "Gets a bounty by id",
					Alias:          []string{"show-bounty"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListThreadMetadata",
					Use:       "list-thread-metadata",
					Short:     "List all thread-metadata",
				},
				{
					RpcMethod:      "GetThreadMetadata",
					Use:            "get-thread-metadata [id]",
					Short:          "Gets a thread-metadata",
					Alias:          []string{"show-thread-metadata"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},
				{
					RpcMethod: "ListThreadFollow",
					Use:       "list-thread-follow",
					Short:     "List all thread-follow",
				},
				{
					RpcMethod:      "GetThreadFollow",
					Use:            "get-thread-follow [id]",
					Short:          "Gets a thread-follow",
					Alias:          []string{"show-thread-follow"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "follower"}},
				},
				{
					RpcMethod: "ListThreadFollowCount",
					Use:       "list-thread-follow-count",
					Short:     "List all thread-follow-count",
				},
				{
					RpcMethod:      "GetThreadFollowCount",
					Use:            "get-thread-follow-count [id]",
					Short:          "Gets a thread-follow-count",
					Alias:          []string{"show-thread-follow-count"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},
				{
					RpcMethod: "ListArchiveMetadata",
					Use:       "list-archive-metadata",
					Short:     "List all archive-metadata",
				},
				{
					RpcMethod:      "GetArchiveMetadata",
					Use:            "get-archive-metadata [id]",
					Short:          "Gets a archive-metadata",
					Alias:          []string{"show-archive-metadata"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod: "ListMemberSalvationStatus",
					Use:       "list-member-salvation-status",
					Short:     "List all member-salvation-status",
				},
				{
					RpcMethod:      "GetMemberSalvationStatus",
					Use:            "get-member-salvation-status [id]",
					Short:          "Gets a member-salvation-status",
					Alias:          []string{"show-member-salvation-status"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListJuryParticipation",
					Use:       "list-jury-participation",
					Short:     "List all jury-participation",
				},
				{
					RpcMethod:      "GetJuryParticipation",
					Use:            "get-jury-participation [id]",
					Short:          "Gets a jury-participation",
					Alias:          []string{"show-jury-participation"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "juror"}},
				},
				{
					RpcMethod: "ListMemberReport",
					Use:       "list-member-report",
					Short:     "List all member-report",
				},
				{
					RpcMethod:      "GetMemberReport",
					Use:            "get-member-report [id]",
					Short:          "Gets a member-report",
					Alias:          []string{"show-member-report"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod: "ListMemberWarning",
					Use:       "list-member-warning",
					Short:     "List all member-warning",
				},
				{
					RpcMethod:      "GetMemberWarning",
					Use:            "get-member-warning [id]",
					Short:          "Gets a member-warning by id",
					Alias:          []string{"show-member-warning"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListGovActionAppeal",
					Use:       "list-gov-action-appeal",
					Short:     "List all gov-action-appeal",
				},
				{
					RpcMethod:      "GetGovActionAppeal",
					Use:            "get-gov-action-appeal [id]",
					Short:          "Gets a gov-action-appeal by id",
					Alias:          []string{"show-gov-action-appeal"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "Posts",
					Use:            "posts [category-id] [status]",
					Short:          "Query posts",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "category_id"}, {ProtoField: "status"}},
				},

				{
					RpcMethod:      "Thread",
					Use:            "thread [root-id]",
					Short:          "Query thread",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},

				{
					RpcMethod:      "Categories",
					Use:            "categories ",
					Short:          "Query categories",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "UserPosts",
					Use:            "user-posts [author]",
					Short:          "Query user-posts",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "author"}},
				},

				{
					RpcMethod:      "SentinelStatus",
					Use:            "sentinel-status [address]",
					Short:          "Query sentinel-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "SentinelBondCommitment",
					Use:            "sentinel-bond-commitment [address]",
					Short:          "Query sentinel-bond-commitment",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "ArchiveCooldown",
					Use:            "archive-cooldown [root-id]",
					Short:          "Query archive-cooldown",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},


				{
					RpcMethod:      "ForumStatus",
					Use:            "forum-status ",
					Short:          "Query forum-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "AppealCooldown",
					Use:            "appeal-cooldown [post-id]",
					Short:          "Query appeal-cooldown",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},

				{
					RpcMethod:      "MemberReports",
					Use:            "member-reports ",
					Short:          "Query member-reports",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "MemberWarnings",
					Use:            "member-warnings [member]",
					Short:          "Query member-warnings",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},

				{
					RpcMethod:      "MemberStanding",
					Use:            "member-standing [member]",
					Short:          "Query member-standing",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},

				{
					RpcMethod:      "PinnedPosts",
					Use:            "pinned-posts [category-id]",
					Short:          "Query pinned-posts",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "category_id"}},
				},

				{
					RpcMethod:      "LockedThreads",
					Use:            "locked-threads ",
					Short:          "Query locked-threads",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "ThreadLockStatus",
					Use:            "thread-lock-status [root-id]",
					Short:          "Query thread-lock-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},

				{
					RpcMethod:      "TopPosts",
					Use:            "top-posts [category-id] [time-range]",
					Short:          "Query top-posts",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "category_id"}, {ProtoField: "time_range"}},
				},

				{
					RpcMethod:      "ThreadFollowers",
					Use:            "thread-followers [thread-id]",
					Short:          "Query thread-followers",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},

				{
					RpcMethod:      "UserFollowedThreads",
					Use:            "user-followed-threads [user]",
					Short:          "Query user-followed-threads",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "user"}},
				},

				{
					RpcMethod:      "IsFollowingThread",
					Use:            "is-following-thread [thread-id] [user]",
					Short:          "Query is-following-thread",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "user"}},
				},

				{
					RpcMethod:      "BountyByThread",
					Use:            "bounty-by-thread [thread-id]",
					Short:          "Query bounty-by-thread",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},

				{
					RpcMethod:      "ActiveBounties",
					Use:            "active-bounties ",
					Short:          "Query active-bounties",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "UserBounties",
					Use:            "user-bounties [user]",
					Short:          "Query user-bounties",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "user"}},
				},

				{
					RpcMethod:      "BountyExpiringSoon",
					Use:            "bounty-expiring-soon [within-seconds]",
					Short:          "Query bounty-expiring-soon",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "within_seconds"}},
				},

				{
					RpcMethod:      "PostFlags",
					Use:            "post-flags [post-id]",
					Short:          "Query post-flags",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},

				{
					RpcMethod:      "FlagReviewQueue",
					Use:            "flag-review-queue ",
					Short:          "Query flag-review-queue",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "GovActionAppeals",
					Use:            "gov-action-appeals ",
					Short:          "Query gov-action-appeals",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true, // only required if you want to use the custom command
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // skipped because authority gated
				},
				{
					RpcMethod: "UpdateOperationalParams",
					Skip:      true, // skipped because council-gated
				},
				{
					RpcMethod:      "CreateCategory",
					Use:            "create-category [title] [description] [members-only-write] [admin-only-write]",
					Short:          "Send a create-category tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "members_only_write"}, {ProtoField: "admin_only_write"}},
				},
				{
					RpcMethod:      "CreatePost",
					Use:            "create-post [category-id] [parent-id] [content] [--tags tags] [--content-type type]",
					Short:          "Send a create-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "category_id"}, {ProtoField: "parent_id"}, {ProtoField: "content"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"content_type": {Name: "content-type", Usage: "content type hint (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
					},
				},
				{
					RpcMethod:      "EditPost",
					Use:            "edit-post [post-id] [new-content] [--tags tags] [--content-type type]",
					Short:          "Send an edit-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "new_content"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"content_type": {Name: "content-type", Usage: "content type hint (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
					},
				},
				{
					RpcMethod:      "DeletePost",
					Use:            "delete-post [post-id]",
					Short:          "Send a delete-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod:      "FreezeThread",
					Use:            "freeze-thread [root-id]",
					Short:          "Send a freeze-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod:      "UnarchiveThread",
					Use:            "unarchive-thread [root-id]",
					Short:          "Send a unarchive-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod:      "PinPost",
					Use:            "pin-post [post-id] [priority]",
					Short:          "Send a pin-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "priority"}},
				},
				{
					RpcMethod:      "UnpinPost",
					Use:            "unpin-post [post-id]",
					Short:          "Send a unpin-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod:      "LockThread",
					Use:            "lock-thread [root-id] [reason]",
					Short:          "Send a lock-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "UnlockThread",
					Use:            "unlock-thread [root-id]",
					Short:          "Send a unlock-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod:      "MoveThread",
					Use:            "move-thread [root-id] [new-category-id] [reason]",
					Short:          "Send a move-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}, {ProtoField: "new_category_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "FollowThread",
					Use:            "follow-thread [thread-id]",
					Short:          "Send a follow-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},
				{
					RpcMethod:      "UnfollowThread",
					Use:            "unfollow-thread [thread-id]",
					Short:          "Send a unfollow-thread tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},
				{
					RpcMethod:      "UpvotePost",
					Use:            "upvote-post [post-id]",
					Short:          "Send a upvote-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod:      "DownvotePost",
					Use:            "downvote-post [post-id]",
					Short:          "Send a downvote-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod:      "FlagPost",
					Use:            "flag-post [post-id] [category] [reason]",
					Short:          "Send a flag-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "category"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "DismissFlags",
					Use:            "dismiss-flags [post-id] [reason]",
					Short:          "Send a dismiss-flags tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "HidePost",
					Use:            "hide-post [post-id] [reason-code] [reason-text]",
					Short:          "Send a hide-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "reason_code"}, {ProtoField: "reason_text"}},
				},
				{
					RpcMethod:      "AppealPost",
					Use:            "appeal-post [post-id]",
					Short:          "Send a appeal-post tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
				},
				{
					RpcMethod:      "AppealThreadLock",
					Use:            "appeal-thread-lock [root-id]",
					Short:          "Send a appeal-thread-lock tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod:      "AppealThreadMove",
					Use:            "appeal-thread-move [root-id]",
					Short:          "Send a appeal-thread-move tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "root_id"}},
				},
				{
					RpcMethod:      "CreateBounty",
					Use:            "create-bounty [thread-id] [amount] [duration]",
					Short:          "Send a create-bounty tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "amount"}, {ProtoField: "duration"}},
				},
				{
					RpcMethod:      "AwardBounty",
					Use:            "award-bounty [bounty-id]",
					Short:          "Send a award-bounty tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "bounty_id"}},
				},
				{
					RpcMethod:      "IncreaseBounty",
					Use:            "increase-bounty [bounty-id] [additional-amount]",
					Short:          "Send a increase-bounty tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "bounty_id"}, {ProtoField: "additional_amount"}},
				},
				{
					RpcMethod:      "CancelBounty",
					Use:            "cancel-bounty [bounty-id]",
					Short:          "Send a cancel-bounty tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "bounty_id"}},
				},
				{
					RpcMethod:      "AssignBountyToReply",
					Use:            "assign-bounty-to-reply [thread-id] [reply-id] [reason]",
					Short:          "Send a assign-bounty-to-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reply_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "PinReply",
					Use:            "pin-reply [thread-id] [reply-id]",
					Short:          "Send a pin-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reply_id"}},
				},
				{
					RpcMethod:      "UnpinReply",
					Use:            "unpin-reply [thread-id] [reply-id]",
					Short:          "Send a unpin-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reply_id"}},
				},
				{
					RpcMethod:      "DisputePin",
					Use:            "dispute-pin [thread-id] [reply-id] [reason]",
					Short:          "Send a dispute-pin tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reply_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "MarkAcceptedReply",
					Use:            "mark-accepted-reply [thread-id] [reply-id]",
					Short:          "Send a mark-accepted-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reply_id"}},
				},
				{
					RpcMethod:      "ConfirmProposedReply",
					Use:            "confirm-proposed-reply [thread-id]",
					Short:          "Send a confirm-proposed-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}},
				},
				{
					RpcMethod:      "RejectProposedReply",
					Use:            "reject-proposed-reply [thread-id] [reason]",
					Short:          "Send a reject-proposed-reply tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "thread_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "SetForumPaused",
					Use:            "set-forum-paused [paused]",
					Short:          "Send a set-forum-paused tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "paused"}},
				},
				{
					RpcMethod:      "SetModerationPaused",
					Use:            "set-moderation-paused [paused]",
					Short:          "Send a set-moderation-paused tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "paused"}},
				},
				{
					RpcMethod:      "BondSentinel",
					Use:            "bond-sentinel [amount]",
					Short:          "Send a bond-sentinel tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "amount"}},
				},
				{
					RpcMethod:      "UnbondSentinel",
					Use:            "unbond-sentinel [amount]",
					Short:          "Send a unbond-sentinel tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "amount"}},
				},
				{
					RpcMethod:      "ReportMember",
					Use:            "report-member [member] [reason] [recommended-action]",
					Short:          "Send a report-member tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}, {ProtoField: "reason"}, {ProtoField: "recommended_action"}},
				},
				{
					RpcMethod:      "CosignMemberReport",
					Use:            "cosign-member-report [member]",
					Short:          "Send a cosign-member-report tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod:      "ResolveMemberReport",
					Use:            "resolve-member-report [member] [action] [reason]",
					Short:          "Send a resolve-member-report tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}, {ProtoField: "action"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "DefendMemberReport",
					Use:            "defend-member-report [defense]",
					Short:          "Send a defend-member-report tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "defense"}},
				},
				{
					RpcMethod:      "AppealGovAction",
					Use:            "appeal-gov-action [action-type] [action-target] [appeal-reason]",
					Short:          "Send a appeal-gov-action tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "action_type"}, {ProtoField: "action_target"}, {ProtoField: "appeal_reason"}},
				},

				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

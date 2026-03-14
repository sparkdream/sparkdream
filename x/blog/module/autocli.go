package blog

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/blog/types"
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
					RpcMethod:      "ShowPost",
					Use:            "show-post [id]",
					Short:          "Query a post by ID",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListPost",
					Use:       "list-post",
					Short:     "List all posts (paginated)",
				},
				{
					RpcMethod:      "ShowReply",
					Use:            "show-reply [id]",
					Short:          "Query a reply by ID",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "ListReplies",
					Use:            "list-replies [post-id]",
					Short:          "List replies for a post (paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"filter_by_parent": {Name: "filter-by-parent", Usage: "filter by parent reply ID"},
						"parent_reply_id":  {Name: "parent-reply-id", Usage: "parent reply ID (0 = top-level only)"},
						"include_hidden":   {Name: "include-hidden", Usage: "include hidden replies"},
					},
				},
				{
					RpcMethod:      "ListPostsByCreator",
					Use:            "list-posts-by-creator [creator]",
					Short:          "List posts by a specific creator (paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "creator"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"include_hidden": {Name: "include-hidden", Usage: "include hidden posts"},
					},
				},
				{
					RpcMethod:      "ReactionCounts",
					Use:            "reaction-counts [post-id]",
					Short:          "Query reaction counts for a post or reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"reply_id": {Name: "reply-id", Usage: "reply ID (0 = post counts)"},
					},
				},
				{
					RpcMethod:      "UserReaction",
					Use:            "user-reaction [creator] [post-id]",
					Short:          "Query a user's reaction on a target",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "creator"}, {ProtoField: "post_id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"reply_id": {Name: "reply-id", Usage: "reply ID (0 = post reaction)"},
					},
				},
				{
					RpcMethod:      "ListReactions",
					Use:            "list-reactions [post-id]",
					Short:          "List reactions for a post or reply (paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"reply_id": {Name: "reply-id", Usage: "reply ID (0 = post reactions)"},
					},
				},
				{
					RpcMethod:      "ListReactionsByCreator",
					Use:            "list-reactions-by-creator [creator]",
					Short:          "List reactions by a specific creator (paginated)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "creator"}},
				},
				{
					RpcMethod: "ListExpiringContent",
					Use:       "list-expiring-content",
					Short:     "List ephemeral content expiring before a timestamp (paginated)",
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"expires_before": {Name: "expires-before", Usage: "Unix timestamp cutoff (0 = block_time + 1 day)"},
						"content_type":   {Name: "content-type", Usage: "filter: 'post', 'reply', or '' (both)"},
					},
				},
				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true,
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
					RpcMethod:      "CreatePost",
					Use:            "create-post [title] [body]",
					Short:          "Create a new blog post",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title"}, {ProtoField: "body"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"content_type":          {Name: "content-type", Usage: "content type (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
						"min_reply_trust_level": {Name: "min-reply-trust-level", Usage: "minimum trust level to reply (-1 to 4, default: 0)"},
					},
				},
				{
					RpcMethod:      "UpdatePost",
					Use:            "update-post [title] [body] [id]",
					Short:          "Update an existing blog post",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title"}, {ProtoField: "body"}, {ProtoField: "id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"content_type":          {Name: "content-type", Usage: "content type (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
						"replies_enabled":       {Name: "replies-enabled", Usage: "whether replies are accepted"},
						"min_reply_trust_level": {Name: "min-reply-trust-level", Usage: "minimum trust level to reply (-1 to 4)"},
					},
				},
				{
					RpcMethod:      "DeletePost",
					Use:            "delete-post [id]",
					Short:          "Tombstone a blog post",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "HidePost",
					Use:            "hide-post [id]",
					Short:          "Hide a post (author self-moderation)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "UnhidePost",
					Use:            "unhide-post [id]",
					Short:          "Restore a hidden post",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "CreateReply",
					Use:            "create-reply [post-id] [body]",
					Short:          "Create a threaded reply to a post",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "body"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"parent_reply_id": {Name: "parent-reply-id", Usage: "parent reply ID for nesting (0 = top-level)"},
						"content_type":    {Name: "content-type", Usage: "content type (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
					},
				},
				{
					RpcMethod:      "UpdateReply",
					Use:            "update-reply [id] [body]",
					Short:          "Update an existing reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}, {ProtoField: "body"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"content_type": {Name: "content-type", Usage: "content type (e.g. CONTENT_TYPE_TEXT, CONTENT_TYPE_MARKDOWN)"},
					},
				},
				{
					RpcMethod:      "DeleteReply",
					Use:            "delete-reply [id]",
					Short:          "Tombstone a reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "HideReply",
					Use:            "hide-reply [id]",
					Short:          "Hide a reply (post author moderation)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "UnhideReply",
					Use:            "unhide-reply [id]",
					Short:          "Restore a hidden reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "React",
					Use:            "react [post-id] [reaction-type]",
					Short:          "Add or change a reaction on a post or reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}, {ProtoField: "reaction_type"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"reply_id": {Name: "reply-id", Usage: "reply ID (0 = react to the post itself)"},
					},
				},
				{
					RpcMethod:      "RemoveReaction",
					Use:            "remove-reaction [post-id]",
					Short:          "Remove your reaction from a post or reply",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "post_id"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"reply_id": {Name: "reply-id", Usage: "reply ID (0 = post reaction)"},
					},
				},
				{
					RpcMethod:      "PinPost",
					Use:            "pin-post [id]",
					Short:          "Pin an ephemeral post, making it permanent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "PinReply",
					Use:            "pin-reply [id]",
					Short:          "Pin an ephemeral reply, making it permanent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

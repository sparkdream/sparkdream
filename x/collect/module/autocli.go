package collect

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/collect/types"
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
					RpcMethod:      "Collection",
					Use:            "collection [id]",
					Short:          "Query Collection",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},

				{
					RpcMethod:      "CollectionsByOwner",
					Use:            "collections-by-owner [owner]",
					Short:          "Query CollectionsByOwner",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "owner"}},
				},

				{
					RpcMethod:      "PublicCollections",
					Use:            "public-collections ",
					Short:          "Query PublicCollections",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "PublicCollectionsByType",
					Use:            "public-collections-by-type [collection-type]",
					Short:          "Query PublicCollectionsByType",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_type"}},
				},

				{
					RpcMethod:      "CollectionsByCollaborator",
					Use:            "collections-by-collaborator [address]",
					Short:          "Query CollectionsByCollaborator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "Item",
					Use:            "item [id]",
					Short:          "Query Item",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},

				{
					RpcMethod:      "Items",
					Use:            "items [collection-id]",
					Short:          "Query Items",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "ItemsByOwner",
					Use:            "items-by-owner [owner]",
					Short:          "Query ItemsByOwner",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "owner"}},
				},

				{
					RpcMethod:      "Collaborators",
					Use:            "collaborators [collection-id]",
					Short:          "Query Collaborators",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "Curator",
					Use:            "curator [address]",
					Short:          "Query Curator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "ActiveCurators",
					Use:            "active-curators ",
					Short:          "Query ActiveCurators",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "CurationSummary",
					Use:            "curation-summary [collection-id]",
					Short:          "Query CurationSummary",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "CurationReviews",
					Use:            "curation-reviews [collection-id]",
					Short:          "Query CurationReviews",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "CurationReviewsByCurator",
					Use:            "curation-reviews-by-curator [curator]",
					Short:          "Query CurationReviewsByCurator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "curator"}},
				},

				{
					RpcMethod:      "SponsorshipRequest",
					Use:            "sponsorship-request [collection-id]",
					Short:          "Query SponsorshipRequest",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "SponsorshipRequests",
					Use:            "sponsorship-requests ",
					Short:          "Query SponsorshipRequests",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "ContentFlag",
					Use:            "content-flag [target-id] [target-type]",
					Short:          "Query ContentFlag",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}},
				},

				{
					RpcMethod:      "FlaggedContent",
					Use:            "flagged-content ",
					Short:          "Query FlaggedContent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "HideRecord",
					Use:            "hide-record [id]",
					Short:          "Query HideRecord",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},

				{
					RpcMethod:      "HideRecordsByTarget",
					Use:            "hide-records-by-target [target-id] [target-type]",
					Short:          "Query HideRecordsByTarget",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}},
				},

				{
					RpcMethod:      "PendingCollections",
					Use:            "pending-collections ",
					Short:          "Query PendingCollections",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "Endorsement",
					Use:            "endorsement [collection-id]",
					Short:          "Query Endorsement",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},

				{
					RpcMethod:      "CollectionsByContent",
					Use:            "collections-by-content [module] [entity-type] [entity-id]",
					Short:          "Query collections referencing on-chain content",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "module"}, {ProtoField: "entity_type"}, {ProtoField: "entity_id"}},
				},

				{
					RpcMethod:      "CollectionConviction",
					Use:            "collection-conviction [collection-id]",
					Short:          "Query conviction score, stakes, and author bond for a collection",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
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
					RpcMethod:      "CreateCollection",
					Use:            "create-collection [type] [visibility] [encrypted] [expires-at] [name] [description] [cover-uri] [tags]",
					Short:          "Send a CreateCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "type"}, {ProtoField: "visibility"}, {ProtoField: "encrypted"}, {ProtoField: "expires_at"}, {ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "cover_uri"}, {ProtoField: "tags"}},
				},
				{
					RpcMethod:      "UpdateCollection",
					Use:            "update-collection [id] [type] [expires-at] [name] [description] [cover-uri] [tags]",
					Short:          "Send a UpdateCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}, {ProtoField: "type"}, {ProtoField: "expires_at"}, {ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "cover_uri"}, {ProtoField: "tags"}},
				},
				{
					RpcMethod:      "DeleteCollection",
					Use:            "delete-collection [id]",
					Short:          "Send a DeleteCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "AddItem",
					Use:            "add-item [collection-id] [position] [title] [description] [image-uri] [reference-type]",
					Short:          "Send a AddItem tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "position"}, {ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "image_uri"}, {ProtoField: "reference_type"}},
				},
				{
					RpcMethod:      "AddItems",
					Use:            "add-items [collection-id]",
					Short:          "Send a AddItems tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				{
					RpcMethod:      "UpdateItem",
					Use:            "update-item [id] [title] [description] [image-uri] [reference-type]",
					Short:          "Send a UpdateItem tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}, {ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "image_uri"}, {ProtoField: "reference_type"}},
				},
				{
					RpcMethod:      "RemoveItem",
					Use:            "remove-item [id]",
					Short:          "Send a RemoveItem tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "RemoveItems",
					Use:            "remove-items ",
					Short:          "Send a RemoveItems tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "ReorderItem",
					Use:            "reorder-item [id] [new-position]",
					Short:          "Send a ReorderItem tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}, {ProtoField: "new_position"}},
				},
				{
					RpcMethod:      "AddCollaborator",
					Use:            "add-collaborator [collection-id] [address] [role]",
					Short:          "Send a AddCollaborator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "address"}, {ProtoField: "role"}},
				},
				{
					RpcMethod:      "RemoveCollaborator",
					Use:            "remove-collaborator [collection-id] [address]",
					Short:          "Send a RemoveCollaborator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "address"}},
				},
				{
					RpcMethod:      "UpdateCollaboratorRole",
					Use:            "update-collaborator-role [collection-id] [address] [role]",
					Short:          "Send a UpdateCollaboratorRole tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "address"}, {ProtoField: "role"}},
				},
				{
					RpcMethod:      "RegisterCurator",
					Use:            "register-curator [bond-amount]",
					Short:          "Send a RegisterCurator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "bond_amount"}},
				},
				{
					RpcMethod:      "UnregisterCurator",
					Use:            "unregister-curator ",
					Short:          "Send a UnregisterCurator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "RateCollection",
					Use:            "rate-collection [collection-id] [verdict] [tags] [comment]",
					Short:          "Send a RateCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "verdict"}, {ProtoField: "tags"}, {ProtoField: "comment"}},
				},
				{
					RpcMethod:      "ChallengeReview",
					Use:            "challenge-review [review-id] [reason]",
					Short:          "Send a ChallengeReview tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "review_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "RequestSponsorship",
					Use:            "request-sponsorship [collection-id]",
					Short:          "Send a RequestSponsorship tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				{
					RpcMethod:      "CancelSponsorshipRequest",
					Use:            "cancel-sponsorship-request [collection-id]",
					Short:          "Send a CancelSponsorshipRequest tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				{
					RpcMethod:      "SponsorCollection",
					Use:            "sponsor-collection [collection-id]",
					Short:          "Send a SponsorCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				{
					RpcMethod:      "UpvoteContent",
					Use:            "upvote-content [target-id] [target-type]",
					Short:          "Send a UpvoteContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}},
				},
				{
					RpcMethod:      "DownvoteContent",
					Use:            "downvote-content [target-id] [target-type]",
					Short:          "Send a DownvoteContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}},
				},
				{
					RpcMethod:      "FlagContent",
					Use:            "flag-content [target-id] [target-type] [reason] [reason-text]",
					Short:          "Send a FlagContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}, {ProtoField: "reason"}, {ProtoField: "reason_text"}},
				},
				{
					RpcMethod:      "HideContent",
					Use:            "hide-content [target-id] [target-type] [reason-code] [reason-text]",
					Short:          "Send a HideContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_id"}, {ProtoField: "target_type"}, {ProtoField: "reason_code"}, {ProtoField: "reason_text"}},
				},
				{
					RpcMethod:      "AppealHide",
					Use:            "appeal-hide [hide-record-id]",
					Short:          "Send a AppealHide tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "hide_record_id"}},
				},
				{
					RpcMethod:      "EndorseCollection",
					Use:            "endorse-collection [collection-id]",
					Short:          "Send a EndorseCollection tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				{
					RpcMethod:      "SetSeekingEndorsement",
					Use:            "set-seeking-endorsement [collection-id] [seeking]",
					Short:          "Send a SetSeekingEndorsement tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}, {ProtoField: "seeking"}},
				},
				{
					RpcMethod:      "PinCollection",
					Use:            "pin-collection [collection-id]",
					Short:          "Pin an ephemeral collection to make it permanent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "collection_id"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

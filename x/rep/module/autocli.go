package rep

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/rep/types"
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
					RpcMethod: "ListMember",
					Use:       "list-member",
					Short:     "List all member",
				},
				{
					RpcMethod:      "GetMember",
					Use:            "get-member [id]",
					Short:          "Gets a member",
					Alias:          []string{"show-member"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListInvitation",
					Use:       "list-invitation",
					Short:     "List all invitation",
				},
				{
					RpcMethod:      "GetInvitation",
					Use:            "get-invitation [id]",
					Short:          "Gets a invitation by id",
					Alias:          []string{"show-invitation"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListProject",
					Use:       "list-project",
					Short:     "List all project",
				},
				{
					RpcMethod:      "GetProject",
					Use:            "get-project [id]",
					Short:          "Gets a project by id",
					Alias:          []string{"show-project"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListInitiative",
					Use:       "list-initiative",
					Short:     "List all initiative",
				},
				{
					RpcMethod:      "GetInitiative",
					Use:            "get-initiative [id]",
					Short:          "Gets a initiative by id",
					Alias:          []string{"show-initiative"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListStake",
					Use:       "list-stake",
					Short:     "List all stake",
				},
				{
					RpcMethod:      "GetStake",
					Use:            "get-stake [id]",
					Short:          "Gets a stake by id",
					Alias:          []string{"show-stake"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListChallenge",
					Use:       "list-challenge",
					Short:     "List all challenge",
				},
				{
					RpcMethod:      "GetChallenge",
					Use:            "get-challenge [id]",
					Short:          "Gets a challenge by id",
					Alias:          []string{"show-challenge"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListJuryReview",
					Use:       "list-jury-review",
					Short:     "List all jury-review",
				},
				{
					RpcMethod:      "GetJuryReview",
					Use:            "get-jury-review [id]",
					Short:          "Gets a jury-review by id",
					Alias:          []string{"show-jury-review"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListInterim",
					Use:       "list-interim",
					Short:     "List all interim",
				},
				{
					RpcMethod:      "GetInterim",
					Use:            "get-interim [id]",
					Short:          "Gets an interim by id",
					Alias:          []string{"show-interim"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListInterimTemplate",
					Use:       "list-interim-template",
					Short:     "List all interim-template",
				},
				{
					RpcMethod:      "GetInterimTemplate",
					Use:            "get-interim-template [id]",
					Short:          "Gets an interim-template",
					Alias:          []string{"show-interim-template"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "template_id"}},
				},
				{
					RpcMethod:      "MembersByTrustLevel",
					Use:            "members-by-trust-level [trust-level]",
					Short:          "Query members-by-trust-level",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "trust_level"}},
				},

				{
					RpcMethod:      "InvitationsByInviter",
					Use:            "invitations-by-inviter [inviter]",
					Short:          "Query invitations-by-inviter",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "inviter"}},
				},

				{
					RpcMethod:      "InterimsByAssignee",
					Use:            "interims-by-assignee [assignee]",
					Short:          "Query interims-by-assignee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "assignee"}},
				},

				{
					RpcMethod:      "InterimsByType",
					Use:            "interims-by-type [interim-type]",
					Short:          "Query interims-by-type",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_type"}},
				},

				{
					RpcMethod:      "InterimsByReference",
					Use:            "interims-by-reference [reference-type] [reference-id]",
					Short:          "Query interims-by-reference",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "reference_type"}, {ProtoField: "reference_id"}},
				},

				{
					RpcMethod:      "ProjectsByCouncil",
					Use:            "projects-by-council [council]",
					Short:          "Query projects-by-council",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "council"}},
				},

				{
					RpcMethod:      "InitiativesByProject",
					Use:            "initiatives-by-project [project-id]",
					Short:          "Query initiatives-by-project",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "project_id"}},
				},

				{
					RpcMethod:      "InitiativesByAssignee",
					Use:            "initiatives-by-assignee [assignee]",
					Short:          "Query initiatives-by-assignee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "assignee"}},
				},

				{
					RpcMethod:      "AvailableInitiatives",
					Use:            "available-initiatives ",
					Short:          "Query available-initiatives",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "StakesByStaker",
					Use:            "stakes-by-staker [staker]",
					Short:          "Query stakes-by-staker",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "staker"}},
				},

				{
					RpcMethod:      "StakesByTarget",
					Use:            "stakes-by-target [target-type] [target-id]",
					Short:          "Query stakes-by-target",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}},
				},

				{
					RpcMethod:      "InitiativeConviction",
					Use:            "initiative-conviction [initiative-id]",
					Short:          "Query initiative-conviction",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}},
				},

				{
					RpcMethod:      "ChallengesByInitiative",
					Use:            "challenges-by-initiative [initiative-id]",
					Short:          "Query challenges-by-initiative",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}},
				},

				{
					RpcMethod:      "Reputation",
					Use:            "reputation [address] [tag]",
					Short:          "Query reputation",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}, {ProtoField: "tag"}},
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
					RpcMethod:      "InviteMember",
					Use:            "invite-member [invitee-address] [staked-dream]",
					Short:          "Send a invite-member tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "invitee_address"}, {ProtoField: "staked_dream"}},
				},
				{
					RpcMethod:      "AcceptInvitation",
					Use:            "accept-invitation [invitation-id]",
					Short:          "Send a accept-invitation tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "invitation_id"}},
				},
				{
					RpcMethod:      "TransferDream",
					Use:            "transfer-dream [recipient] [amount] [purpose] [reference]",
					Short:          "Send a transfer-dream tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "recipient"}, {ProtoField: "amount"}, {ProtoField: "purpose"}, {ProtoField: "reference"}},
				},
				{
					RpcMethod:      "ProposeProject",
					Use:            "propose-project [name] [description] [category] [council] [requested-budget] [requested-spark]",
					Short:          "Send a propose-project tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "category"}, {ProtoField: "council"}, {ProtoField: "requested_budget"}, {ProtoField: "requested_spark"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"tags":         {Name: "tags"},
						"deliverables": {Name: "deliverables"},
						"milestones":   {Name: "milestones"},
					},
				},
				{
					RpcMethod:      "ApproveProjectBudget",
					Use:            "approve-project-budget [project-id] [approved-budget] [approved-spark]",
					Short:          "Send a approve-project-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "project_id"}, {ProtoField: "approved_budget"}, {ProtoField: "approved_spark"}},
				},
				{
					RpcMethod:      "CancelProject",
					Use:            "cancel-project [project-id] [reason]",
					Short:          "Send a cancel-project tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "project_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "CreateInitiative",
					Use:            "create-initiative [project-id] [title] [description] [tier] [category] [template-id] [budget]",
					Short:          "Send a create-initiative tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "project_id"}, {ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "tier"}, {ProtoField: "category"}, {ProtoField: "template_id"}, {ProtoField: "budget"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"tags": {Name: "tags"},
					},
				},
				{
					RpcMethod:      "AssignInitiative",
					Use:            "assign-initiative [initiative-id] [assignee]",
					Short:          "Send a assign-initiative tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "assignee"}},
				},
				{
					RpcMethod:      "SubmitInitiativeWork",
					Use:            "submit-initiative-work [initiative-id] [deliverable-uri] [comments]",
					Short:          "Send a submit-initiative-work tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "deliverable_uri"}, {ProtoField: "comments"}},
				},
				{
					RpcMethod:      "ApproveInitiative",
					Use:            "approve-initiative [initiative-id] [approved] [comments]",
					Short:          "Send a approve-initiative tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "approved"}, {ProtoField: "comments"}},
				},
				{
					RpcMethod:      "AbandonInitiative",
					Use:            "abandon-initiative [initiative-id] [reason]",
					Short:          "Send a abandon-initiative tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "CompleteInitiative",
					Use:            "complete-initiative [initiative-id] [completion-notes]",
					Short:          "Send a complete-initiative tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "completion_notes"}},
				},
				{
					RpcMethod:      "Stake",
					Use:            "stake [target-type] [target-id] [amount]",
					Short:          "Send a stake tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "Unstake",
					Use:            "unstake [stake-id] [amount]",
					Short:          "Send a unstake tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "stake_id"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "CreateChallenge",
					Use:            "create-challenge [initiative-id] [reason] [staked-dream] [is-anonymous] [payout-address]",
					Short:          "Send a create-challenge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "reason"}, {ProtoField: "staked_dream"}, {ProtoField: "is_anonymous"}, {ProtoField: "payout_address"}},
				},
				{
					RpcMethod:      "RespondToChallenge",
					Use:            "respond-to-challenge [challenge-id] [response]",
					Short:          "Send a respond-to-challenge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "challenge_id"}, {ProtoField: "response"}},
				},
				{
					RpcMethod:      "SubmitJurorVote",
					Use:            "submit-juror-vote [jury-review-id] [verdict] [confidence] [reasoning]",
					Short:          "Send a submit-juror-vote tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "jury_review_id"}, {ProtoField: "verdict"}, {ProtoField: "confidence"}, {ProtoField: "reasoning"}},
					FlagOptions: map[string]*autocliv1.FlagOptions{
						"criteria_votes": {Name: "criteria-votes"},
					},
				},
				{
					RpcMethod:      "SubmitExpertTestimony",
					Use:            "submit-expert-testimony [jury-review-id] [opinion] [reasoning]",
					Short:          "Send a submit-expert-testimony tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "jury_review_id"}, {ProtoField: "opinion"}, {ProtoField: "reasoning"}},
				},
				{
					RpcMethod:      "AssignInterim",
					Use:            "assign-interim [interim-id] [assignee]",
					Short:          "Send a assign-interim tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_id"}, {ProtoField: "assignee"}},
				},
				{
					RpcMethod:      "SubmitInterimWork",
					Use:            "submit-interim-work [interim-id] [deliverable-uri] [comments]",
					Short:          "Send a submit-interim-work tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_id"}, {ProtoField: "deliverable_uri"}, {ProtoField: "comments"}},
				},
				{
					RpcMethod:      "ApproveInterim",
					Use:            "approve-interim [interim-id] [approved] [comments]",
					Short:          "Send a approve-interim tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_id"}, {ProtoField: "approved"}, {ProtoField: "comments"}},
				},
				{
					RpcMethod:      "AbandonInterim",
					Use:            "abandon-interim [interim-id] [reason]",
					Short:          "Send a abandon-interim tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "CompleteInterim",
					Use:            "complete-interim [interim-id] [completion-notes]",
					Short:          "Send a complete-interim tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_id"}, {ProtoField: "completion_notes"}},
				},
				{
					RpcMethod:      "CreateInterim",
					Use:            "create-interim [interim-type] [reference-id] [reference-type] [complexity] [deadline]",
					Short:          "Send a create-interim tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "interim_type"}, {ProtoField: "reference_id"}, {ProtoField: "reference_type"}, {ProtoField: "complexity"}, {ProtoField: "deadline"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

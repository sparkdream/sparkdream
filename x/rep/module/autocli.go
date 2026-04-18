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

				{
					RpcMethod:      "ContentConviction",
					Use:            "content-conviction [target-type] [target-id]",
					Short:          "Query content conviction score",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}},
				},

				{
					RpcMethod:      "AuthorBond",
					Use:            "author-bond [target-type] [target-id]",
					Short:          "Query author bond for content",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}},
				},

				{
					RpcMethod:      "GetContentChallenge",
					Use:            "get-content-challenge [id]",
					Short:          "Gets a content challenge by id",
					Alias:          []string{"show-content-challenge"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListContentChallenge",
					Use:       "list-content-challenge",
					Short:     "List all content challenges",
				},
				{
					RpcMethod:      "ContentChallengesByTarget",
					Use:            "content-challenges-by-target [target-type] [target-id]",
					Short:          "Query content challenges by target",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}},
				},

				{
					RpcMethod:      "ContentByInitiative",
					Use:            "content-by-initiative [initiative-id]",
					Short:          "Query content items linked to an initiative for conviction propagation",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}},
				},
				{
					RpcMethod: "ListTag",
					Use:       "list-tag",
					Short:     "List all tags",
				},
				{
					RpcMethod:      "GetTag",
					Use:            "get-tag [name]",
					Short:          "Gets a tag",
					Alias:          []string{"show-tag"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod: "ListReservedTag",
					Use:       "list-reserved-tag",
					Short:     "List all reserved tags",
				},
				{
					RpcMethod:      "GetReservedTag",
					Use:            "get-reserved-tag [name]",
					Short:          "Gets a reserved tag",
					Alias:          []string{"show-reserved-tag"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "TagExists",
					Use:            "tag-exists [tag-name]",
					Short:          "Check whether a tag exists",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag_name"}},
				},
				{
					RpcMethod: "ListTagReport",
					Use:       "list-tag-report",
					Short:     "List all tag reports",
				},
				{
					RpcMethod:      "GetTagReport",
					Use:            "get-tag-report [tag-name]",
					Short:          "Gets a tag report",
					Alias:          []string{"show-tag-report"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag_name"}},
				},
				{
					RpcMethod:      "TagReports",
					Use:            "tag-reports",
					Short:          "Query the first tag report (summary)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod: "ListTagBudget",
					Use:       "list-tag-budget",
					Short:     "List all tag-budget",
				},
				{
					RpcMethod:      "GetTagBudget",
					Use:            "get-tag-budget [id]",
					Short:          "Gets a tag-budget by id",
					Alias:          []string{"show-tag-budget"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListTagBudgetAward",
					Use:       "list-tag-budget-award",
					Short:     "List all tag-budget-award",
				},
				{
					RpcMethod:      "GetTagBudgetAward",
					Use:            "get-tag-budget-award [id]",
					Short:          "Gets a tag-budget-award by id",
					Alias:          []string{"show-tag-budget-award"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "TagBudgetByTag",
					Use:            "tag-budget-by-tag [tag]",
					Short:          "Query tag-budget-by-tag",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag"}},
				},
				{
					RpcMethod:      "TagBudgets",
					Use:            "tag-budgets",
					Short:          "Query tag-budgets (summary)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "TagBudgetAwards",
					Use:            "tag-budget-awards [budget-id]",
					Short:          "Query tag-budget-awards (summary)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "budget_id"}},
				},
				{
					RpcMethod: "ListSentinelActivity",
					Use:       "list-sentinel-activity",
					Short:     "List all sentinel-activity records",
				},
				{
					RpcMethod:      "GetSentinelActivity",
					Use:            "get-sentinel-activity [address]",
					Short:          "Gets a sentinel-activity record",
					Alias:          []string{"show-sentinel-activity"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "SentinelStatus",
					Use:            "sentinel-status [address]",
					Short:          "Query sentinel-status (bond status + current bond)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "SentinelBondCommitment",
					Use:            "sentinel-bond-commitment [address]",
					Short:          "Query sentinel bond commitment (current/committed/available)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListMemberReport",
					Use:       "list-member-report",
					Short:     "List all member-report",
				},
				{
					RpcMethod:      "GetMemberReport",
					Use:            "get-member-report [member]",
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
					RpcMethod: "ListJuryParticipation",
					Use:       "list-jury-participation",
					Short:     "List all jury-participation",
				},
				{
					RpcMethod:      "GetJuryParticipation",
					Use:            "get-jury-participation [juror]",
					Short:          "Gets a jury-participation",
					Alias:          []string{"show-jury-participation"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "juror"}},
				},
				{
					RpcMethod:      "MemberReports",
					Use:            "member-reports",
					Short:          "Query first member report (summary)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "MemberWarnings",
					Use:            "member-warnings [member]",
					Short:          "Query first warning for a member (summary)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod:      "MemberStanding",
					Use:            "member-standing [member]",
					Short:          "Query member standing (warning count, active report, trust tier)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
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
					Use:            "create-challenge [initiative-id] [reason] [staked-dream]",
					Short:          "Send a create-challenge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "initiative_id"}, {ProtoField: "reason"}, {ProtoField: "staked_dream"}},
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
				{
					RpcMethod:      "ChallengeContent",
					Use:            "challenge-content [target-type] [target-id] [reason] [staked-dream]",
					Short:          "Challenge bonded content (blog post, forum post, collection)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target_type"}, {ProtoField: "target_id"}, {ProtoField: "reason"}, {ProtoField: "staked_dream"}},
				},
				{
					RpcMethod:      "RespondToContentChallenge",
					Use:            "respond-to-content-challenge [content-challenge-id] [response]",
					Short:          "Respond to a content challenge as the author",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_challenge_id"}, {ProtoField: "response"}},
				},
				{
					RpcMethod:      "RegisterZkPublicKey",
					Use:            "register-zk-public-key [zk-public-key]",
					Short:          "Send a RegisterZkPublicKey tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "zk_public_key", Varargs: true}},
				},
				{
					RpcMethod:      "CreateTag",
					Use:            "create-tag [name]",
					Short:          "Register a new tag in the x/rep tag registry",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "ReportTag",
					Use:            "report-tag [tag-name] [reason]",
					Short:          "Report a tag for moderation review",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag_name"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ResolveTagReport",
					Use:            "resolve-tag-report [tag-name] [action] [reserve-authority] [reserve-members-can-use]",
					Short:          "Resolve a tag report (0=dismiss, 1=remove, 2=reserve)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag_name"}, {ProtoField: "action"}, {ProtoField: "reserve_authority"}, {ProtoField: "reserve_members_can_use"}},
				},
				{
					RpcMethod:      "CreateTagBudget",
					Use:            "create-tag-budget [tag] [initial-pool] [members-only]",
					Short:          "Send a create-tag-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "tag"}, {ProtoField: "initial_pool"}, {ProtoField: "members_only"}},
				},
				{
					RpcMethod:      "AwardFromTagBudget",
					Use:            "award-from-tag-budget [budget-id] [post-id] [amount] [reason]",
					Short:          "Send a award-from-tag-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "budget_id"}, {ProtoField: "post_id"}, {ProtoField: "amount"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "TopUpTagBudget",
					Use:            "top-up-tag-budget [budget-id] [amount]",
					Short:          "Send a top-up-tag-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "budget_id"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "ToggleTagBudget",
					Use:            "toggle-tag-budget [budget-id] [active]",
					Short:          "Send a toggle-tag-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "budget_id"}, {ProtoField: "active"}},
				},
				{
					RpcMethod:      "WithdrawTagBudget",
					Use:            "withdraw-tag-budget [budget-id]",
					Short:          "Send a withdraw-tag-budget tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "budget_id"}},
				},
				{
					RpcMethod:      "BondSentinel",
					Use:            "bond-sentinel [amount]",
					Short:          "Bond DREAM to register as a sentinel",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "amount"}},
				},
				{
					RpcMethod:      "UnbondSentinel",
					Use:            "unbond-sentinel [amount]",
					Short:          "Unbond DREAM from sentinel record",
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
				{
					RpcMethod:      "ResolveGovActionAppeal",
					Use:            "resolve-gov-action-appeal [appeal-id] [verdict] [reason]",
					Short:          "Resolve a pending gov-action appeal (verdict: UPHELD or OVERTURNED)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "appeal_id"}, {ProtoField: "verdict"}, {ProtoField: "reason"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

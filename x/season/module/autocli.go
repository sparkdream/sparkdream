package season

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/season/types"
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
					RpcMethod: "GetSeason",
					Use:       "get-season",
					Short:     "Gets a season",
					Alias:     []string{"show-season"},
				},
				{
					RpcMethod: "GetSeasonTransitionState",
					Use:       "get-season-transition-state",
					Short:     "Gets a season-transition-state",
					Alias:     []string{"show-season-transition-state"},
				},
				{
					RpcMethod: "GetTransitionRecoveryState",
					Use:       "get-transition-recovery-state",
					Short:     "Gets a transition-recovery-state",
					Alias:     []string{"show-transition-recovery-state"},
				},
				{
					RpcMethod: "GetNextSeasonInfo",
					Use:       "get-next-season-info",
					Short:     "Gets a next-season-info",
					Alias:     []string{"show-next-season-info"},
				},
				{
					RpcMethod: "ListSeasonSnapshot",
					Use:       "list-season-snapshot",
					Short:     "List all season-snapshot",
				},
				{
					RpcMethod:      "GetSeasonSnapshot",
					Use:            "get-season-snapshot [id]",
					Short:          "Gets a season-snapshot",
					Alias:          []string{"show-season-snapshot"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "season"}},
				},
				{
					RpcMethod: "ListMemberSeasonSnapshot",
					Use:       "list-member-season-snapshot",
					Short:     "List all member-season-snapshot",
				},
				{
					RpcMethod:      "GetMemberSeasonSnapshot",
					Use:            "get-member-season-snapshot [id]",
					Short:          "Gets a member-season-snapshot",
					Alias:          []string{"show-member-season-snapshot"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "season_address"}},
				},
				{
					RpcMethod: "ListMemberProfile",
					Use:       "list-member-profile",
					Short:     "List all member-profile",
				},
				{
					RpcMethod:      "GetMemberProfile",
					Use:            "get-member-profile [id]",
					Short:          "Gets a member-profile",
					Alias:          []string{"show-member-profile"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListMemberRegistration",
					Use:       "list-member-registration",
					Short:     "List all member-registration",
				},
				{
					RpcMethod:      "GetMemberRegistration",
					Use:            "get-member-registration [id]",
					Short:          "Gets a member-registration",
					Alias:          []string{"show-member-registration"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod: "ListAchievement",
					Use:       "list-achievement",
					Short:     "List all achievement",
				},
				{
					RpcMethod:      "GetAchievement",
					Use:            "get-achievement [id]",
					Short:          "Gets a achievement",
					Alias:          []string{"show-achievement"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "achievement_id"}},
				},
				{
					RpcMethod: "ListTitle",
					Use:       "list-title",
					Short:     "List all title",
				},
				{
					RpcMethod:      "GetTitle",
					Use:            "get-title [id]",
					Short:          "Gets a title",
					Alias:          []string{"show-title"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title_id"}},
				},
				{
					RpcMethod: "ListSeasonTitleEligibility",
					Use:       "list-season-title-eligibility",
					Short:     "List all season-title-eligibility",
				},
				{
					RpcMethod:      "GetSeasonTitleEligibility",
					Use:            "get-season-title-eligibility [id]",
					Short:          "Gets a season-title-eligibility",
					Alias:          []string{"show-season-title-eligibility"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title_season"}},
				},
				{
					RpcMethod: "ListGuild",
					Use:       "list-guild",
					Short:     "List all guild",
				},
				{
					RpcMethod:      "GetGuild",
					Use:            "get-guild [id]",
					Short:          "Gets a guild by id",
					Alias:          []string{"show-guild"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListGuildMembership",
					Use:       "list-guild-membership",
					Short:     "List all guild-membership",
				},
				{
					RpcMethod:      "GetGuildMembership",
					Use:            "get-guild-membership [id]",
					Short:          "Gets a guild-membership",
					Alias:          []string{"show-guild-membership"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod: "ListGuildInvite",
					Use:       "list-guild-invite",
					Short:     "List all guild-invite",
				},
				{
					RpcMethod:      "GetGuildInvite",
					Use:            "get-guild-invite [id]",
					Short:          "Gets a guild-invite",
					Alias:          []string{"show-guild-invite"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_invitee"}},
				},
				{
					RpcMethod: "ListQuest",
					Use:       "list-quest",
					Short:     "List all quest",
				},
				{
					RpcMethod:      "GetQuest",
					Use:            "get-quest [id]",
					Short:          "Gets a quest",
					Alias:          []string{"show-quest"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},
				{
					RpcMethod: "ListMemberQuestProgress",
					Use:       "list-member-quest-progress",
					Short:     "List all member-quest-progress",
				},
				{
					RpcMethod:      "GetMemberQuestProgress",
					Use:            "get-member-quest-progress [id]",
					Short:          "Gets a member-quest-progress",
					Alias:          []string{"show-member-quest-progress"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member_quest"}},
				},
				{
					RpcMethod: "ListEpochXpTracker",
					Use:       "list-epoch-xp-tracker",
					Short:     "List all epoch-xp-tracker",
				},
				{
					RpcMethod:      "GetEpochXpTracker",
					Use:            "get-epoch-xp-tracker [id]",
					Short:          "Gets a epoch-xp-tracker",
					Alias:          []string{"show-epoch-xp-tracker"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member_epoch"}},
				},
				{
					RpcMethod: "ListVoteXpRecord",
					Use:       "list-vote-xp-record",
					Short:     "List all vote-xp-record",
				},
				{
					RpcMethod:      "GetVoteXpRecord",
					Use:            "get-vote-xp-record [id]",
					Short:          "Gets a vote-xp-record",
					Alias:          []string{"show-vote-xp-record"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "season_member_proposal"}},
				},
				{
					RpcMethod: "ListForumXpCooldown",
					Use:       "list-forum-xp-cooldown",
					Short:     "List all forum-xp-cooldown",
				},
				{
					RpcMethod:      "GetForumXpCooldown",
					Use:            "get-forum-xp-cooldown [id]",
					Short:          "Gets a forum-xp-cooldown",
					Alias:          []string{"show-forum-xp-cooldown"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "beneficiary_actor"}},
				},
				{
					RpcMethod: "ListDisplayNameModeration",
					Use:       "list-display-name-moderation",
					Short:     "List all display-name-moderation",
				},
				{
					RpcMethod:      "GetDisplayNameModeration",
					Use:            "get-display-name-moderation [id]",
					Short:          "Gets a display-name-moderation",
					Alias:          []string{"show-display-name-moderation"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},
				{
					RpcMethod: "ListDisplayNameReportStake",
					Use:       "list-display-name-report-stake",
					Short:     "List all display-name-report-stake",
				},
				{
					RpcMethod:      "GetDisplayNameReportStake",
					Use:            "get-display-name-report-stake [id]",
					Short:          "Gets a display-name-report-stake",
					Alias:          []string{"show-display-name-report-stake"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "challenge_id"}},
				},
				{
					RpcMethod: "ListDisplayNameAppealStake",
					Use:       "list-display-name-appeal-stake",
					Short:     "List all display-name-appeal-stake",
				},
				{
					RpcMethod:      "GetDisplayNameAppealStake",
					Use:            "get-display-name-appeal-stake [id]",
					Short:          "Gets a display-name-appeal-stake",
					Alias:          []string{"show-display-name-appeal-stake"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "challenge_id"}},
				},
				{
					RpcMethod:      "CurrentSeason",
					Use:            "current-season ",
					Short:          "Query current-season",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "SeasonByNumber",
					Use:            "season-by-number [number]",
					Short:          "Query season-by-number",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "number"}},
				},

				{
					RpcMethod:      "SeasonStats",
					Use:            "season-stats [season]",
					Short:          "Query season-stats",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "season"}},
				},

				{
					RpcMethod:      "MemberByDisplayName",
					Use:            "member-by-display-name [display-name]",
					Short:          "Query member-by-display-name",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "display_name"}},
				},

				{
					RpcMethod:      "MemberSeasonHistory",
					Use:            "member-season-history [address]",
					Short:          "Query member-season-history",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "MemberXpHistory",
					Use:            "member-xp-history [address] [season] [epochs-back]",
					Short:          "Query member-xp-history",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}, {ProtoField: "season"}, {ProtoField: "epochs_back"}},
				},

				{
					RpcMethod:      "Achievements",
					Use:            "achievements ",
					Short:          "Query achievements",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "MemberAchievements",
					Use:            "member-achievements [address]",
					Short:          "Query member-achievements",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "Titles",
					Use:            "titles ",
					Short:          "Query titles",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "MemberTitles",
					Use:            "member-titles [address]",
					Short:          "Query member-titles",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "GuildById",
					Use:            "guild-by-id [guild-id]",
					Short:          "Query guild-by-id",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},

				{
					RpcMethod:      "GuildsList",
					Use:            "guilds-list ",
					Short:          "Query guilds-list",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "GuildsByFounder",
					Use:            "guilds-by-founder [founder] [include-dissolved]",
					Short:          "Query guilds-by-founder",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "founder"}, {ProtoField: "include_dissolved"}},
				},

				{
					RpcMethod:      "GuildMembers",
					Use:            "guild-members [guild-id]",
					Short:          "Query guild-members",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},

				{
					RpcMethod:      "MemberGuild",
					Use:            "member-guild [member]",
					Short:          "Query member-guild",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},

				{
					RpcMethod:      "GuildInvites",
					Use:            "guild-invites [guild-id]",
					Short:          "Query guild-invites",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},

				{
					RpcMethod:      "MemberGuildInvites",
					Use:            "member-guild-invites [member]",
					Short:          "Query member-guild-invites",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}},
				},

				{
					RpcMethod:      "QuestsList",
					Use:            "quests-list ",
					Short:          "Query quests-list",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "QuestById",
					Use:            "quest-by-id [quest-id]",
					Short:          "Query quest-by-id",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},

				{
					RpcMethod:      "QuestChain",
					Use:            "quest-chain [quest-chain]",
					Short:          "Query quest-chain",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_chain"}},
				},

				{
					RpcMethod:      "MemberQuestStatus",
					Use:            "member-quest-status [member] [quest-id]",
					Short:          "Query member-quest-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "member"}, {ProtoField: "quest_id"}},
				},

				{
					RpcMethod:      "AvailableQuests",
					Use:            "available-quests [member]",
					Short:          "Query available-quests",
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
					RpcMethod:      "SetDisplayName",
					Use:            "set-display-name [name]",
					Short:          "Send a set-display-name tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "SetUsername",
					Use:            "set-username [username]",
					Short:          "Send a set-username tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "username"}},
				},
				{
					RpcMethod:      "SetDisplayTitle",
					Use:            "set-display-title [title-id]",
					Short:          "Send a set-display-title tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title_id"}},
				},
				{
					RpcMethod:      "CreateGuild",
					Use:            "create-guild [name] [description] [invite-only]",
					Short:          "Send a create-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "invite_only"}},
				},
				{
					RpcMethod:      "JoinGuild",
					Use:            "join-guild [guild-id]",
					Short:          "Send a join-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},
				{
					RpcMethod:      "LeaveGuild",
					Use:            "leave-guild ",
					Short:          "Send a leave-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "TransferGuildFounder",
					Use:            "transfer-guild-founder [guild-id] [new-founder]",
					Short:          "Send a transfer-guild-founder tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "new_founder"}},
				},
				{
					RpcMethod:      "DissolveGuild",
					Use:            "dissolve-guild [guild-id]",
					Short:          "Send a dissolve-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},
				{
					RpcMethod:      "PromoteToOfficer",
					Use:            "promote-to-officer [guild-id] [member]",
					Short:          "Send a promote-to-officer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "member"}},
				},
				{
					RpcMethod:      "DemoteOfficer",
					Use:            "demote-officer [guild-id] [officer]",
					Short:          "Send a demote-officer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "officer"}},
				},
				{
					RpcMethod:      "InviteToGuild",
					Use:            "invite-to-guild [guild-id] [invitee]",
					Short:          "Send a invite-to-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "invitee"}},
				},
				{
					RpcMethod:      "AcceptGuildInvite",
					Use:            "accept-guild-invite [guild-id]",
					Short:          "Send a accept-guild-invite tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},
				{
					RpcMethod:      "RevokeGuildInvite",
					Use:            "revoke-guild-invite [guild-id] [invitee]",
					Short:          "Send a revoke-guild-invite tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "invitee"}},
				},
				{
					RpcMethod:      "SetGuildInviteOnly",
					Use:            "set-guild-invite-only [guild-id] [invite-only]",
					Short:          "Send a set-guild-invite-only tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "invite_only"}},
				},
				{
					RpcMethod:      "UpdateGuildDescription",
					Use:            "update-guild-description [guild-id] [description]",
					Short:          "Send a update-guild-description tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "description"}},
				},
				{
					RpcMethod:      "KickFromGuild",
					Use:            "kick-from-guild [guild-id] [member] [reason]",
					Short:          "Send a kick-from-guild tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}, {ProtoField: "member"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ClaimGuildFounder",
					Use:            "claim-guild-founder [guild-id]",
					Short:          "Send a claim-guild-founder tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "guild_id"}},
				},
				{
					RpcMethod:      "StartQuest",
					Use:            "start-quest [quest-id]",
					Short:          "Send a start-quest tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},
				{
					RpcMethod:      "ClaimQuestReward",
					Use:            "claim-quest-reward [quest-id]",
					Short:          "Send a claim-quest-reward tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},
				{
					RpcMethod:      "AbandonQuest",
					Use:            "abandon-quest [quest-id]",
					Short:          "Send a abandon-quest tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},
				{
					RpcMethod:      "CreateQuest",
					Use:            "create-quest [quest-id] [name] [description] [xp-reward] [repeatable] [cooldown-epochs] [season] [start-block] [end-block] [min-level] [required-achievement] [prerequisite-quest] [quest-chain]",
					Short:          "Send a create-quest tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}, {ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "xp_reward"}, {ProtoField: "repeatable"}, {ProtoField: "cooldown_epochs"}, {ProtoField: "season"}, {ProtoField: "start_block"}, {ProtoField: "end_block"}, {ProtoField: "min_level"}, {ProtoField: "required_achievement"}, {ProtoField: "prerequisite_quest"}, {ProtoField: "quest_chain"}},
				},
				{
					RpcMethod:      "DeactivateQuest",
					Use:            "deactivate-quest [quest-id]",
					Short:          "Send a deactivate-quest tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "quest_id"}},
				},
				{
					RpcMethod:      "ExtendSeason",
					Use:            "extend-season [extension-epochs] [reason]",
					Short:          "Send a extend-season tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "extension_epochs"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "SetNextSeasonInfo",
					Use:            "set-next-season-info [name] [theme]",
					Short:          "Send a set-next-season-info tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "theme"}},
				},
				{
					RpcMethod:      "AbortSeasonTransition",
					Use:            "abort-season-transition ",
					Short:          "Send a abort-season-transition tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "RetrySeasonTransition",
					Use:            "retry-season-transition ",
					Short:          "Send a retry-season-transition tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "SkipTransitionPhase",
					Use:            "skip-transition-phase ",
					Short:          "Send a skip-transition-phase tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "ReportDisplayName",
					Use:            "report-display-name [target] [reason]",
					Short:          "Send a report-display-name tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "target"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "AppealDisplayNameModeration",
					Use:            "appeal-display-name-moderation [appeal-reason]",
					Short:          "Send a appeal-display-name-moderation tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "appeal_reason"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

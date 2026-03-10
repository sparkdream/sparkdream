package commons

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/commons/types"
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
					RpcMethod: "ListPolicyPermissions",
					Use:       "list-policy-permissions",
					Short:     "List all policyPermissions",
				},
				{
					RpcMethod:      "GetPolicyPermissions",
					Use:            "get-policy-permissions [id]",
					Short:          "Gets a policyPermissions",
					Alias:          []string{"show-policy-permissions"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "policy_address"}},
				},
				{
					RpcMethod: "ListGroups",
					Use:       "list-group",
					Short:     "List all groups",
				},
				{
					RpcMethod:      "GetGroup",
					Use:            "get-group [id]",
					Short:          "Gets a group",
					Alias:          []string{"show-group"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod:      "GetCouncilMembers",
					Use:            "get-council-members [council-name]",
					Short:          "Get members of a council",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "council_name"}},
				},
				{
					RpcMethod:      "GetProposal",
					Use:            "get-proposal [proposal-id]",
					Short:          "Get a proposal by ID with votes and tally",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod: "ListProposals",
					Use:       "list-proposals",
					Short:     "List all proposals",
				},
				{
					RpcMethod:      "GetProposalVotes",
					Use:            "get-proposal-votes [proposal-id]",
					Short:          "Get votes for a proposal",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
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
					RpcMethod:      "SpendFromCommons",
					Use:            "spend-from-commons [recipient] [amount]",
					Short:          "Send a spend-from-commons tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "recipient"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "EmergencyCancelGovProposal",
					Use:            "emergency-cancel-proposal [proposal-id]",
					Short:          "Send a emergency_cancel_proposal tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod:      "CreatePolicyPermissions",
					Use:            "create-policy-permissions [policy_address] [allowed-messages]",
					Short:          "Create a new policyPermissions",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "policy_address"}, {ProtoField: "allowed_messages", Varargs: true}},
				},
				{
					RpcMethod:      "UpdatePolicyPermissions",
					Use:            "update-policy-permissions [policy_address] [allowed-messages]",
					Short:          "Update policyPermissions",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "policy_address"}, {ProtoField: "allowed_messages", Varargs: true}},
				},
				{
					RpcMethod:      "DeletePolicyPermissions",
					Use:            "delete-policy-permissions [policy_address]",
					Short:          "Delete policyPermissions",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "policy_address"}},
				},
				{
					RpcMethod:      "RegisterGroup",
					Use:            "register-group [name] [description] [members] [member-weights] [funding-weight] [max-spend-per-epoch] [update-cooldown] [vote-threshold] [futarchy-enabled] [intended-parent-address] [min-members] [max-members] [term-duration] [activation-time]",
					Short:          "Send a register-group tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "description"}, {ProtoField: "members"}, {ProtoField: "member_weights"}, {ProtoField: "funding_weight"}, {ProtoField: "max_spend_per_epoch"}, {ProtoField: "update_cooldown"}, {ProtoField: "vote_threshold"}, {ProtoField: "futarchy_enabled"}, {ProtoField: "intended_parent_address"}, {ProtoField: "min_members"}, {ProtoField: "max_members"}, {ProtoField: "term_duration"}, {ProtoField: "activation_time"}},
				},
				{
					RpcMethod:      "RenewGroup",
					Use:            "renew-group [group-name] [new-members] [new-member-weights]",
					Short:          "Send a renew-group tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "group_name"}, {ProtoField: "new_members"}, {ProtoField: "new_member_weights"}},
				},
				{
					RpcMethod:      "UpdateGroupConfig",
					Use:            "update-group-config [group-name] [max-spend-per-epoch] [update-cooldown] [futarchy-enabled] [min-members] [max-members] [term-duration]",
					Short:          "Send a update-group-config tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "group_name"}, {ProtoField: "max_spend_per_epoch"}, {ProtoField: "update_cooldown"}, {ProtoField: "futarchy_enabled"}, {ProtoField: "min_members"}, {ProtoField: "max_members"}, {ProtoField: "term_duration"}},
				},
				{
					RpcMethod:      "UpdateGroupMembers",
					Use:            "update-group-members [policy-address] [members-to-add] [weights-to-add] [members-to-remove]",
					Short:          "Send a update-group-members tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "group_policy_address"}, {ProtoField: "members_to_add"}, {ProtoField: "weights_to_add"}, {ProtoField: "members_to_remove"}},
				},
				{
					RpcMethod:      "ForceUpgrade",
					Use:            "force-upgrade [plan]",
					Short:          "Send a force-upgrade tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "plan"}},
				},
				{
					RpcMethod:      "DeleteGroup",
					Use:            "delete-group [group-name]",
					Short:          "Send a DeleteGroup tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "group_name"}},
				},
				{
					RpcMethod:      "VetoGroupProposals",
					Use:            "veto-group-proposals [group-name]",
					Short:          "Send a veto-group-proposals tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "group_name"}},
				},
				{
					RpcMethod: "SubmitProposal",
					Skip:      true, // custom CLI command (handles JSON file with Any messages)
				},
				{
					RpcMethod:      "VoteProposal",
					Use:            "vote-proposal [proposal-id] [option]",
					Short:          "Vote on a proposal (VOTE_OPTION_YES, VOTE_OPTION_NO, VOTE_OPTION_ABSTAIN, VOTE_OPTION_NO_WITH_VETO)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "option"}},
				},
				{
					RpcMethod:      "ExecuteProposal",
					Use:            "execute-proposal [proposal-id]",
					Short:          "Execute an accepted proposal",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

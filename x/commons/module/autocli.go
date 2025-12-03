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
					RpcMethod:      "EmergencyCancelProposal",
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
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

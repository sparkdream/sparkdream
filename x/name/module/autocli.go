package name

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/name/types"
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
					RpcMethod:      "Resolve",
					Use:            "resolve [name]",
					Short:          "Query resolve",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},

				{
					RpcMethod:      "ReverseResolve",
					Use:            "reverse-resolve [address]",
					Short:          "Query reverse-resolve",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "Names",
					Use:            "names [address]",
					Short:          "Query names",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod: "ListDispute",
					Use:       "list-dispute",
					Short:     "List all dispute",
				},
				{
					RpcMethod:      "GetDispute",
					Use:            "get-dispute [id]",
					Short:          "Gets a dispute",
					Alias:          []string{"show-dispute"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "GetOwnerInfo",
					Use:            "owner-info [address]",
					Short:          "Get owner info (primary_name, display_name, last_active_time) for an address",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "Targets",
					Use:            "targets [address]",
					Short:          "List names where the given address is the accepted resolver target",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
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
					RpcMethod:      "RegisterName",
					Use:            "register-name [name] [data]",
					Short:          "Send a register-name tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "data"}},
				},
				{
					RpcMethod:      "SetPrimary",
					Use:            "set-primary [name]",
					Short:          "Send a set-primary tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "FileDispute",
					Use:            "file-dispute [name] [reason]",
					Short:          "Send a file-dispute tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ContestDispute",
					Use:            "contest-dispute [name] [reason]",
					Short:          "Send a contest-dispute tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ResolveDispute",
					Use:            "resolve-dispute [name] [new-owner] [transfer-approved]",
					Short:          "Send a resolve-dispute tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "new_owner"}, {ProtoField: "transfer_approved"}},
				},
				{
					RpcMethod:      "UpdateName",
					Use:            "update-name [name] [data]",
					Short:          "Send a update-name tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "data"}},
				},
				{
					RpcMethod:      "SetDisplayName",
					Use:            "set-display-name [display-name]",
					Short:          "Set (or clear, with empty string) the free-form display name on the signer's OwnerInfo",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "display_name"}},
				},
				{
					RpcMethod:      "SetTarget",
					Use:            "set-target [name] [target]",
					Short:          "Point a name at a target address for forward resolution (empty target clears)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "target"}},
				},
				{
					RpcMethod:      "AcceptTarget",
					Use:            "accept-target [name]",
					Short:          "Consent to being the resolver target of a name",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}},
				},
				{
					RpcMethod:      "TransferName",
					Use:            "transfer-name [name] [new-owner]",
					Short:          "Transfer ownership of a name to a new owner (must be an active x/rep member)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "name"}, {ProtoField: "new_owner"}},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

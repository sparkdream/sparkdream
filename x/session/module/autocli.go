package session

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/session/types"
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
					RpcMethod:      "Session",
					Use:            "session [granter] [grantee]",
					Short:          "Query a single session by granter and grantee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "granter"}, {ProtoField: "grantee"}},
				},
				{
					RpcMethod:      "SessionsByGranter",
					Use:            "sessions-by-granter [granter]",
					Short:          "Query all active sessions for a granter",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "granter"}},
				},
				{
					RpcMethod:      "SessionsByGrantee",
					Use:            "sessions-by-grantee [grantee]",
					Short:          "Query all active sessions for a grantee",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grantee"}},
				},
				{
					RpcMethod: "AllowedMsgTypes",
					Use:       "allowed-msg-types",
					Short:     "Query the ceiling and currently active delegable message types",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // authority gated
				},
				{
					RpcMethod: "UpdateOperationalParams",
					Skip:      true, // authority gated
				},
				{
					RpcMethod:      "CreateSession",
					Use:            "create-session [grantee] [allowed-msg-types] [spend-limit] [expiration] [max-exec-count]",
					Short:          "Create a new session key delegation",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grantee"}, {ProtoField: "allowed_msg_types"}, {ProtoField: "spend_limit"}, {ProtoField: "expiration"}, {ProtoField: "max_exec_count"}},
				},
				{
					RpcMethod:      "RevokeSession",
					Use:            "revoke-session [grantee]",
					Short:          "Revoke an active session",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grantee"}},
				},
				{
					RpcMethod: "ExecSession",
					Skip:      true, // requires custom CLI to construct Any-encoded inner messages
				},
			},
		},
	}
}

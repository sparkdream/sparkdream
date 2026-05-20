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
				{
					RpcMethod:      "Grant",
					Use:            "grant [id]",
					Short:          "Query a single grant by id",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "GrantsByGranter",
					Use:            "grants-by-granter [granter]",
					Short:          "Query all active grants for a granter (any type)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "granter"}},
				},
				{
					RpcMethod:      "GrantsByGrantee",
					Use:            "grants-by-grantee [grantee]",
					Short:          "Query all active grants for a grantee (any type)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grantee"}},
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
				{
					RpcMethod: "CreateGrant",
					Skip:      true, // payload oneof needs the umbrella CLI; lands with P5b
				},
				{
					RpcMethod:      "ClaimRecurringPull",
					Use:            "claim-recurring-pull [grant-id]",
					Short:          "Claim one period of a RECURRING_PULL grant (signed by the grantee)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grant_id"}},
				},
				{
					RpcMethod:      "PullAllowance",
					Use:            "pull-allowance [grant-id] [recipient] [amount]",
					Short:          "Pull funds from a SPENDING_ALLOWANCE grant to a chosen recipient (signed by the grantee)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grant_id"}, {ProtoField: "recipient"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "RetryScheduledOneshot",
					Use:            "retry-oneshot [grant-id]",
					Short:          "Re-enqueue a PAUSED ScheduledOneshot for firing (caller must be the granter or grantee)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grant_id"}},
				},
				{
					RpcMethod:      "RevokeGrant",
					Use:            "revoke-grant [grant-id]",
					Short:          "Revoke any active grant by id (signed by the granter, or by a session key with allow_self_revoke = true)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grant_id"}},
				},
				{
					RpcMethod:      "DeclineGrant",
					Use:            "decline-grant [grant-id]",
					Short:          "Decline an active grant as the grantee (one-way; refunds any held oneshot deposit to the granter)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "grant_id"}},
				},
			},
		},
	}
}

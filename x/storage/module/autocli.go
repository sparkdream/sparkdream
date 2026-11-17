package storage

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/storage/types"
)

func pos(fields ...string) []*autocliv1.PositionalArgDescriptor {
	out := make([]*autocliv1.PositionalArgDescriptor, 0, len(fields))
	for _, f := range fields {
		out = append(out, &autocliv1.PositionalArgDescriptor{ProtoField: f})
	}
	return out
}

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Shows the parameters of the module"},
				{RpcMethod: "Backend", Use: "backend [name]", Short: "Show one backend", PositionalArgs: pos("name")},
				{RpcMethod: "Backends", Use: "backends", Short: "List the backends"},
				{RpcMethod: "ApprovedOperators", Use: "approved-operators [backend]", Short: "List a backend's approved storage operators", PositionalArgs: pos("backend")},
				{RpcMethod: "ApprovedScanners", Use: "approved-scanners", Short: "List the approved content scanners"},
				{RpcMethod: "OperatorStanding", Use: "operator-standing [backend] [operator]", Short: "Show whether an operator could claim now and what it has left in this window", PositionalArgs: pos("backend", "operator")},
				{RpcMethod: "Request", Use: "request [id]", Short: "Show one request", PositionalArgs: pos("id")},
				{RpcMethod: "RequestByHash", Use: "request-by-hash [backend] [sha256]", Short: "Show the live or settled request for a file", PositionalArgs: pos("backend", "sha256")},
				{RpcMethod: "RequestsByStatus", Use: "requests-by-status [backend] [status]", Short: "List a backend's requests in one status", PositionalArgs: pos("backend", "status")},
				{RpcMethod: "RequestsByRequester", Use: "requests-by-requester [requester]", Short: "List a member's requests", PositionalArgs: pos("requester")},
				{RpcMethod: "RequestsByOperator", Use: "requests-by-operator [operator]", Short: "List an operator's open claims and unsettled completions", PositionalArgs: pos("operator")},
				{RpcMethod: "RemovedHash", Use: "removed-hash [sha256]", Short: "Show the flags on a hash", PositionalArgs: pos("sha256")},
				{RpcMethod: "QuoteFee", Use: "quote-fee [backend] [content-type] [size-bytes]", Short: "Show what a request would cost now", PositionalArgs: pos("backend", "content_type", "size_bytes")},
				{RpcMethod: "RequestLocation", Use: "request-location [id]", Short: "Show a request's source addresses and locators, even when its hash is flagged", PositionalArgs: pos("id")},
				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true, // only required if you want to use the custom command
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "UpdateParams", Skip: true}, // skipped because authority gated
				{
					RpcMethod:      "RequestPermanentCopy",
					Use:            "request-permanent-copy [backend] [sha256] [size-bytes] [content-type] [url] [max-fee]",
					Short:          "Request a permanent copy of an off-chain file (see request-file-copy to start from a local file)",
					PositionalArgs: pos("backend", "sha256", "size_bytes", "content_type", "url", "max_fee"),
				},
				{RpcMethod: "AddSource", Use: "add-source [request-id] [url] [max-fee]", Short: "Add an address a request's file can be fetched from", PositionalArgs: pos("request_id", "url", "max_fee")},
				{RpcMethod: "CancelRequest", Use: "cancel-request [request-id]", Short: "Withdraw a pending request", PositionalArgs: pos("request_id")},
				{RpcMethod: "DisputeCompletion", Use: "dispute-completion [request-id] [max-fee]", Short: "Hold a completion's settlement for the dispute window", PositionalArgs: pos("request_id", "max_fee")},
				{RpcMethod: "ClaimRequest", Use: "claim-request [request-id]", Short: "Take a pending request (storage operator)", PositionalArgs: pos("request_id")},
				{RpcMethod: "ExtendClaim", Use: "extend-claim [request-id]", Short: "Extend a claim once (storage operator)", PositionalArgs: pos("request_id")},
				{
					RpcMethod:      "CompleteRequest",
					Use:            "complete-request [request-id] [locator]",
					Short:          "Record where the copy is (storage operator); a scanned request needs --attestations and --verdicts-locator",
					PositionalArgs: pos("request_id", "locator"),
				},
				{RpcMethod: "DeclineRequest", Use: "decline-request [request-id] [reason]", Short: "Decline a request (storage operator); reason is unreachable or mismatch", PositionalArgs: pos("request_id", "reason")},
				{RpcMethod: "FlagRemoved", Use: "flag-removed [sha256] [attestation-hash]", Short: "Flag a hash as removed content (content scanner)", PositionalArgs: pos("sha256", "attestation_hash")},
				// Signed by a council policy or x/gov: sent through a proposal.
				{RpcMethod: "UnflagRemoved", Skip: true},
				{RpcMethod: "VoidCompletion", Skip: true},
				{RpcMethod: "ApproveOperator", Skip: true},
				{RpcMethod: "RemoveOperator", Skip: true},
				{RpcMethod: "ApproveScanner", Skip: true},
				{RpcMethod: "RemoveScanner", Skip: true},
				{RpcMethod: "UpdateBackend", Skip: true},
				{RpcMethod: "UpdateOperationalParams", Skip: true},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

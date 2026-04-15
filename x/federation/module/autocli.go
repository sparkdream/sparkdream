package federation

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/federation/types"
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
					RpcMethod:      "GetPeer",
					Use:            "get-peer [peer-id]",
					Short:          "Query GetPeer",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ListPeers",
					Use:            "list-peers ",
					Short:          "Query ListPeers",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "GetPeerPolicy",
					Use:            "get-peer-policy [peer-id]",
					Short:          "Query GetPeerPolicy",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "GetBridgeOperator",
					Use:            "get-bridge-operator [address] [peer-id]",
					Short:          "Query GetBridgeOperator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}, {ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ListBridgeOperators",
					Use:            "list-bridge-operators ",
					Short:          "Query ListBridgeOperators",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "GetFederatedContent",
					Use:            "get-federated-content [id]",
					Short:          "Query GetFederatedContent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod:      "ListFederatedContent",
					Use:            "list-federated-content ",
					Short:          "Query ListFederatedContent",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "GetIdentityLink",
					Use:            "get-identity-link [local-address] [peer-id]",
					Short:          "Query GetIdentityLink",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "local_address"}, {ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ListIdentityLinks",
					Use:            "list-identity-links ",
					Short:          "Query ListIdentityLinks",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "ResolveRemoteIdentity",
					Use:            "resolve-remote-identity [peer-id] [remote-identity]",
					Short:          "Query ResolveRemoteIdentity",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "remote_identity"}},
				},
				{
					RpcMethod:      "GetPendingIdentityChallenge",
					Use:            "get-pending-identity-challenge [claimed-address] [peer-id]",
					Short:          "Query GetPendingIdentityChallenge",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "claimed_address"}, {ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ListPendingIdentityChallenges",
					Use:            "list-pending-identity-challenges [claimed-address]",
					Short:          "Query ListPendingIdentityChallenges",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "claimed_address"}},
				},
				{
					RpcMethod:      "GetReputationAttestation",
					Use:            "get-reputation-attestation [local-address] [peer-id]",
					Short:          "Query GetReputationAttestation",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "local_address"}, {ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ListOutboundAttestations",
					Use:            "list-outbound-attestations ",
					Short:          "Query ListOutboundAttestations",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "GetVerifier",
					Use:            "get-verifier [address]",
					Short:          "Query GetVerifier",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "ListVerifiers",
					Use:            "list-verifiers ",
					Short:          "Query ListVerifiers",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "GetVerificationRecord",
					Use:            "get-verification-record [content-id]",
					Short:          "Query GetVerificationRecord",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}},
				},
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
					RpcMethod:      "RegisterPeer",
					Use:            "register-peer [peer-id] [display-name] [metadata] [ibc-channel-id]",
					Short:          "Send a RegisterPeer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "display_name"}, {ProtoField: "metadata"}, {ProtoField: "ibc_channel_id"}},
				},
				{
					RpcMethod:      "RemovePeer",
					Use:            "remove-peer [peer-id] [reason]",
					Short:          "Send a RemovePeer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "SuspendPeer",
					Use:            "suspend-peer [peer-id] [reason]",
					Short:          "Send a SuspendPeer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ResumePeer",
					Use:            "resume-peer [peer-id]",
					Short:          "Send a ResumePeer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "UpdatePeerPolicy",
					Use:            "update-peer-policy [peer-id]",
					Short:          "Send a UpdatePeerPolicy tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "RegisterBridge",
					Use:            "register-bridge [operator] [peer-id] [protocol] [endpoint]",
					Short:          "Send a RegisterBridge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "peer_id"}, {ProtoField: "protocol"}, {ProtoField: "endpoint"}},
				},
				{
					RpcMethod:      "RevokeBridge",
					Use:            "revoke-bridge [operator] [peer-id] [reason]",
					Short:          "Send a RevokeBridge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "peer_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "SlashBridge",
					Use:            "slash-bridge [operator] [peer-id] [amount] [reason]",
					Short:          "Send a SlashBridge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "peer_id"}, {ProtoField: "amount"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "UpdateBridge",
					Use:            "update-bridge [operator] [peer-id] [endpoint]",
					Short:          "Send a UpdateBridge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "peer_id"}, {ProtoField: "endpoint"}},
				},
				{
					RpcMethod:      "UnbondBridge",
					Use:            "unbond-bridge [peer-id]",
					Short:          "Send a UnbondBridge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "TopUpBridgeStake",
					Use:            "top-up-bridge-stake [peer-id]",
					Short:          "Send a TopUpBridgeStake tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "SubmitFederatedContent",
					Use:            "submit-federated-content [peer-id] [remote-content-id] [content-type] [creator-identity] [creator-name] [title] [body] [content-uri] [remote-created-at]",
					Short:          "Send a SubmitFederatedContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "remote_content_id"}, {ProtoField: "content_type"}, {ProtoField: "creator_identity"}, {ProtoField: "creator_name"}, {ProtoField: "title"}, {ProtoField: "body"}, {ProtoField: "content_uri"}, {ProtoField: "remote_created_at"}},
				},
				{
					RpcMethod:      "FederateContent",
					Use:            "federate-content [peer-id] [content-type] [local-content-id] [title] [body] [content-uri]",
					Short:          "Send a FederateContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "content_type"}, {ProtoField: "local_content_id"}, {ProtoField: "title"}, {ProtoField: "body"}, {ProtoField: "content_uri"}},
				},
				{
					RpcMethod:      "AttestOutbound",
					Use:            "attest-outbound [peer-id] [content-type] [local-content-id]",
					Short:          "Send a AttestOutbound tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "content_type"}, {ProtoField: "local_content_id"}},
				},
				{
					RpcMethod:      "ModerateContent",
					Use:            "moderate-content [content-id] [reason]",
					Short:          "Send a ModerateContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "LinkIdentity",
					Use:            "link-identity [peer-id] [remote-identity]",
					Short:          "Send a LinkIdentity tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "remote_identity"}},
				},
				{
					RpcMethod:      "UnlinkIdentity",
					Use:            "unlink-identity [peer-id]",
					Short:          "Send a UnlinkIdentity tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}},
				},
				{
					RpcMethod:      "ConfirmIdentityLink",
					Use:            "confirm-identity-link [claimant-chain-peer-id]",
					Short:          "Send a ConfirmIdentityLink tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "claimant_chain_peer_id"}},
				},
				{
					RpcMethod:      "RequestReputationAttestation",
					Use:            "request-reputation-attestation [peer-id] [remote-address]",
					Short:          "Send a RequestReputationAttestation tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "peer_id"}, {ProtoField: "remote_address"}},
				},
				{
					RpcMethod:      "BondVerifier",
					Use:            "bond-verifier [amount]",
					Short:          "Send a BondVerifier tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "amount"}},
				},
				{
					RpcMethod:      "UnbondVerifier",
					Use:            "unbond-verifier [amount]",
					Short:          "Send a UnbondVerifier tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "amount"}},
				},
				{
					RpcMethod:      "VerifyContent",
					Use:            "verify-content [content-id]",
					Short:          "Send a VerifyContent tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}},
				},
				{
					RpcMethod:      "ChallengeVerification",
					Use:            "challenge-verification [content-id] [evidence]",
					Short:          "Send a ChallengeVerification tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}, {ProtoField: "evidence"}},
				},
				{
					RpcMethod:      "SubmitArbiterHash",
					Use:            "submit-arbiter-hash [content-id]",
					Short:          "Send a SubmitArbiterHash tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}},
				},
				{
					RpcMethod:      "EscalateChallenge",
					Use:            "escalate-challenge [content-id]",
					Short:          "Send a EscalateChallenge tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "content_id"}},
				},
				{
					RpcMethod:      "UpdateOperationalParams",
					Use:            "update-operational-params ",
					Short:          "Send a UpdateOperationalParams tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
			},
		},
	}
}

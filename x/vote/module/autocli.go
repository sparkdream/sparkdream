package vote

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/vote/types"
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
					RpcMethod: "ListVotingProposal",
					Use:       "list-voting-proposal",
					Short:     "List all voting-proposal",
				},
				{
					RpcMethod:      "GetVotingProposal",
					Use:            "get-voting-proposal [id]",
					Short:          "Gets a voting-proposal by id",
					Alias:          []string{"show-voting-proposal"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "id"}},
				},
				{
					RpcMethod: "ListVoterRegistration",
					Use:       "list-voter-registration",
					Short:     "List all voter-registration",
				},
				{
					RpcMethod:      "GetVoterRegistration",
					Use:            "get-voter-registration [id]",
					Short:          "Gets a voter-registration",
					Alias:          []string{"show-voter-registration"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod: "ListAnonymousVote",
					Use:       "list-anonymous-vote",
					Short:     "List all anonymous-vote",
				},
				{
					RpcMethod:      "GetAnonymousVote",
					Use:            "get-anonymous-vote [id]",
					Short:          "Gets a anonymous-vote",
					Alias:          []string{"show-anonymous-vote"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod: "ListSealedVote",
					Use:       "list-sealed-vote",
					Short:     "List all sealed-vote",
				},
				{
					RpcMethod:      "GetSealedVote",
					Use:            "get-sealed-vote [id]",
					Short:          "Gets a sealed-vote",
					Alias:          []string{"show-sealed-vote"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod: "ListVoterTreeSnapshot",
					Use:       "list-voter-tree-snapshot",
					Short:     "List all voter-tree-snapshot",
				},
				{
					RpcMethod:      "GetVoterTreeSnapshot",
					Use:            "get-voter-tree-snapshot [id]",
					Short:          "Gets a voter-tree-snapshot",
					Alias:          []string{"show-voter-tree-snapshot"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod: "ListUsedNullifier",
					Use:       "list-used-nullifier",
					Short:     "List all used-nullifier",
				},
				{
					RpcMethod:      "GetUsedNullifier",
					Use:            "get-used-nullifier [id]",
					Short:          "Gets a used-nullifier",
					Alias:          []string{"show-used-nullifier"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod: "ListUsedProposalNullifier",
					Use:       "list-used-proposal-nullifier",
					Short:     "List all used-proposal-nullifier",
				},
				{
					RpcMethod:      "GetUsedProposalNullifier",
					Use:            "get-used-proposal-nullifier [id]",
					Short:          "Gets a used-proposal-nullifier",
					Alias:          []string{"show-used-proposal-nullifier"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod: "ListTleValidatorShare",
					Use:       "list-tle-validator-share",
					Short:     "List all tle-validator-share",
				},
				{
					RpcMethod:      "GetTleValidatorShare",
					Use:            "get-tle-validator-share [id]",
					Short:          "Gets a tle-validator-share",
					Alias:          []string{"show-tle-validator-share"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "validator"}},
				},
				{
					RpcMethod: "ListTleDecryptionShare",
					Use:       "list-tle-decryption-share",
					Short:     "List all tle-decryption-share",
				},
				{
					RpcMethod:      "GetTleDecryptionShare",
					Use:            "get-tle-decryption-share [id]",
					Short:          "Gets a tle-decryption-share",
					Alias:          []string{"show-tle-decryption-share"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					RpcMethod: "ListEpochDecryptionKey",
					Use:       "list-epoch-decryption-key",
					Short:     "List all epoch-decryption-key",
				},
				{
					RpcMethod:      "GetEpochDecryptionKey",
					Use:            "get-epoch-decryption-key [id]",
					Short:          "Gets a epoch-decryption-key",
					Alias:          []string{"show-epoch-decryption-key"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}},
				},
				{
					RpcMethod: "GetSrsState",
					Use:       "get-srs-state",
					Short:     "Gets a srs-state",
					Alias:     []string{"show-srs-state"},
				},
				{
					RpcMethod:      "Proposal",
					Use:            "proposal [proposal-id]",
					Short:          "Query proposal",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},

				{
					RpcMethod:      "Proposals",
					Use:            "proposals ",
					Short:          "Query proposals",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "ProposalsByStatus",
					Use:            "proposals-by-status [status]",
					Short:          "Query proposals-by-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "status"}},
				},

				{
					RpcMethod:      "ProposalsByType",
					Use:            "proposals-by-type [proposal-type]",
					Short:          "Query proposals-by-type",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_type"}},
				},

				{
					RpcMethod:      "ProposalTally",
					Use:            "proposal-tally [proposal-id]",
					Short:          "Query proposal-tally",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},

				{
					RpcMethod:      "ProposalVotes",
					Use:            "proposal-votes [proposal-id]",
					Short:          "Query proposal-votes",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},

				{
					RpcMethod:      "VoterRegistrationQuery",
					Use:            "voter-registration-query [address]",
					Short:          "Query voter-registration-query",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},

				{
					RpcMethod:      "VoterRegistrations",
					Use:            "voter-registrations ",
					Short:          "Query voter-registrations",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "VoterTreeSnapshotQuery",
					Use:            "voter-tree-snapshot-query [proposal-id]",
					Short:          "Query voter-tree-snapshot-query",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},

				{
					RpcMethod:      "VoterMerkleProof",
					Use:            "voter-merkle-proof [proposal-id] [public-key]",
					Short:          "Query voter-merkle-proof",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "public_key"}},
				},

				{
					RpcMethod:      "NullifierUsed",
					Use:            "nullifier-used [proposal-id] [nullifier]",
					Short:          "Query nullifier-used",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "nullifier"}},
				},

				{
					RpcMethod:      "ProposalNullifierUsed",
					Use:            "proposal-nullifier-used [epoch] [nullifier]",
					Short:          "Query proposal-nullifier-used",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}, {ProtoField: "nullifier"}},
				},

				{
					RpcMethod:      "TleStatus",
					Use:            "tle-status ",
					Short:          "Query tle-status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "EpochDecryptionKeyQuery",
					Use:            "epoch-decryption-key-query [epoch]",
					Short:          "Query epoch-decryption-key-query",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}},
				},

				{
					RpcMethod:      "TleValidatorShares",
					Use:            "tle-validator-shares ",
					Short:          "Query tle-validator-shares",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "TleLiveness",
					Use:            "tle-liveness",
					Short:          "Query TLE validator liveness summary across the miss window",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},

				{
					RpcMethod:      "TleValidatorLiveness",
					Use:            "tle-validator-liveness [validator]",
					Short:          "Query TLE liveness for a specific validator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "validator"}},
				},

				{
					RpcMethod:      "GetTleEpochParticipation",
					Use:            "get-tle-epoch-participation [epoch]",
					Short:          "Gets TLE participation record for an epoch",
					Alias:          []string{"show-tle-epoch-participation"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}},
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
					RpcMethod:      "RegisterVoter",
					Use:            "register-voter ",
					Short:          "Send a register-voter tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "DeactivateVoter",
					Use:            "deactivate-voter ",
					Short:          "Send a deactivate-voter tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "RotateVoterKey",
					Use:            "rotate-voter-key ",
					Short:          "Send a rotate-voter-key tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "CreateAnonymousProposal",
					Use:            "create-anonymous-proposal [title] [description] [proposal-type] [reference-id] [voting-period-epochs] [quorum] [threshold] [veto-threshold] [visibility] [nonce] [claimed-epoch]",
					Short:          "Send a create-anonymous-proposal tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "proposal_type"}, {ProtoField: "reference_id"}, {ProtoField: "voting_period_epochs"}, {ProtoField: "quorum"}, {ProtoField: "threshold"}, {ProtoField: "veto_threshold"}, {ProtoField: "visibility"}, {ProtoField: "nonce"}, {ProtoField: "claimed_epoch"}},
				},
				{
					RpcMethod:      "CreateProposal",
					Use:            "create-proposal [title] [description] [proposal-type] [reference-id] [voting-period-epochs] [quorum] [threshold] [veto-threshold] [visibility]",
					Short:          "Send a create-proposal tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "title"}, {ProtoField: "description"}, {ProtoField: "proposal_type"}, {ProtoField: "reference_id"}, {ProtoField: "voting_period_epochs"}, {ProtoField: "quorum"}, {ProtoField: "threshold"}, {ProtoField: "veto_threshold"}, {ProtoField: "visibility"}},
				},
				{
					RpcMethod:      "CancelProposal",
					Use:            "cancel-proposal [proposal-id] [reason]",
					Short:          "Send a cancel-proposal tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "Vote",
					Use:            "vote [proposal-id] [vote-option]",
					Short:          "Send a vote tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "vote_option"}},
				},
				{
					RpcMethod:      "SealedVote",
					Use:            "sealed-vote [proposal-id]",
					Short:          "Send a sealed-vote tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod:      "RevealVote",
					Use:            "reveal-vote [proposal-id] [vote-option]",
					Short:          "Send a reveal-vote tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}, {ProtoField: "vote_option"}},
				},
				{
					RpcMethod:      "SubmitDecryptionShare",
					Use:            "submit-decryption-share [epoch]",
					Short:          "Send a submit-decryption-share tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}},
				},
				{
					RpcMethod:      "RegisterTLEShare",
					Use:            "register-tle-share [share-index]",
					Short:          "Send a register-tle-share tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "share_index"}},
				},
				{
					RpcMethod:      "StoreSRS",
					Use:            "store-srs ",
					Short:          "Send a store-srs tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

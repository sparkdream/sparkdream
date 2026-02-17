package reveal

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/reveal/types"
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
					RpcMethod:      "Contribution",
					Use:            "contribution [contribution-id]",
					Short:          "Query a contribution by ID",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}},
				},
				{
					RpcMethod: "Contributions",
					Use:       "contributions",
					Short:     "Query all contributions",
				},
				{
					RpcMethod:      "ContributionsByContributor",
					Use:            "contributions-by-contributor [contributor]",
					Short:          "Query contributions by contributor address",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contributor"}},
				},
				{
					RpcMethod:      "ContributionsByStatus",
					Use:            "contributions-by-status [status]",
					Short:          "Query contributions by status",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "status"}},
				},
				{
					RpcMethod:      "Tranche",
					Use:            "tranche [contribution-id] [tranche-id]",
					Short:          "Query a single tranche",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}},
				},
				{
					RpcMethod:      "TrancheTally",
					Use:            "tranche-tally [contribution-id] [tranche-id]",
					Short:          "Query verification tally for a tranche",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}},
				},
				{
					RpcMethod:      "TrancheStakes",
					Use:            "tranche-stakes [contribution-id] [tranche-id]",
					Short:          "Query all stakes for a tranche",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}},
				},
				{
					RpcMethod:      "StakeDetail",
					Use:            "stake-detail [stake-id]",
					Short:          "Query a single stake by ID",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "stake_id"}},
				},
				{
					RpcMethod:      "StakesByStaker",
					Use:            "stakes-by-staker [staker]",
					Short:          "Query all stakes by a staker",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "staker"}},
				},
				{
					RpcMethod:      "VotesByVoter",
					Use:            "votes-by-voter [voter]",
					Short:          "Query all verification votes by a voter",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "voter"}},
				},
				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // skipped because authority gated
				},
				{
					RpcMethod:      "Propose",
					Use:            "propose [project-name] [description] [total-valuation] [initial-license] [final-license]",
					Short:          "Propose a new contribution for progressive reveal",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "project_name"}, {ProtoField: "description"}, {ProtoField: "total_valuation"}, {ProtoField: "initial_license"}, {ProtoField: "final_license"}},
				},
				{
					RpcMethod: "Approve",
					Skip:      true, // authority gated (Commons Council proposal)
				},
				{
					RpcMethod: "Reject",
					Skip:      true, // authority gated (Commons Council proposal)
				},
				{
					RpcMethod:      "Stake",
					Use:            "stake [contribution-id] [tranche-id] [amount]",
					Short:          "Stake DREAM toward a tranche to show conviction",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}, {ProtoField: "amount"}},
				},
				{
					RpcMethod:      "Withdraw",
					Use:            "withdraw [stake-id]",
					Short:          "Withdraw a stake",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "stake_id"}},
				},
				{
					RpcMethod:      "Reveal",
					Use:            "reveal [contribution-id] [tranche-id] [code-uri] [docs-uri] [commit-hash]",
					Short:          "Reveal code for a backed tranche",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}, {ProtoField: "code_uri"}, {ProtoField: "docs_uri"}, {ProtoField: "commit_hash"}},
				},
				{
					RpcMethod:      "Verify",
					Use:            "verify [contribution-id] [tranche-id] [value-confirmed] [quality-rating] [comments]",
					Short:          "Vote on verification of revealed code",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "tranche_id"}, {ProtoField: "value_confirmed"}, {ProtoField: "quality_rating"}, {ProtoField: "comments"}},
				},
				{
					RpcMethod:      "Cancel",
					Use:            "cancel [contribution-id] [reason]",
					Short:          "Cancel a contribution",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "contribution_id"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod: "ResolveDispute",
					Skip:      true, // authority gated (Commons Council proposal)
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

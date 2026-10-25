package vault

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/vault/types"
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
					RpcMethod:      "VaultByAddress",
					Use:            "vault-by-address [address]",
					Short:          "Look up a vault by its address",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "VaultsBySigner",
					Use:            "vaults-by-signer [address]",
					Short:          "List the vaults an address (or any of its migrated addresses) signs for",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
				},
				{
					RpcMethod:      "ProposalsByVault",
					Use:            "proposals-by-vault [vault-id]",
					Short:          "List a vault's proposals (--status filters)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "vault_id"}},
				},
				{
					RpcMethod:      "Tally",
					Use:            "tally [proposal-id]",
					Short:          "Live tally of a proposal: approved weight, threshold, met",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod: "ListVault",
					Use:       "list-vault",
					Short:     "List all vault",
				},
				{
					RpcMethod:      "GetVault",
					Use:            "get-vault [id]",
					Short:          "Gets a vault",
					Alias:          []string{"show-vault"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "vault_id"}},
				},
				{
					RpcMethod: "ListVaultProposal",
					Use:       "list-vault-proposal",
					Short:     "List all vault-proposal",
				},
				{
					RpcMethod:      "GetVaultProposal",
					Use:            "get-vault-proposal [id]",
					Short:          "Gets a vault-proposal",
					Alias:          []string{"show-vault-proposal"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
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
					RpcMethod: "CreateVault",
					Skip:      true, // hand-written (repeated VaultSigner), client/cli/tx.go
				},
				{
					RpcMethod: "Propose",
					Skip:      true, // hand-written (repeated Any), client/cli/tx.go
				},
				{
					RpcMethod: "Approve",
					Skip:      true, // hand-written (hex messages_hash), client/cli/tx.go
				},
				{
					RpcMethod:      "RevokeApproval",
					Use:            "revoke-approval [proposal-id]",
					Short:          "Withdraw your approval from a pending vault proposal",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod:      "Execute",
					Use:            "execute [proposal-id]",
					Short:          "Execute a pending vault proposal whose tally meets the threshold (you pay the gas)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod:      "CancelProposal",
					Use:            "cancel-proposal [proposal-id]",
					Short:          "Cancel a pending vault proposal you proposed",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "proposal_id"}},
				},
				{
					RpcMethod: "UpdateVault",
					Skip:      true, // signed by the vault address: only reachable as an inner message of a proposal
				},
			},
		},
	}
}

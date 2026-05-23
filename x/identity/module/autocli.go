package identity

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/identity/types"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface. No Tx
// surface; queries only (chain-identity, bond-denom, dream-denom).
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "ChainIdentity",
					Use:       "chain-identity",
					Short:     "Show the chain's immutable identity record",
				},
				{
					RpcMethod: "BondDenom",
					Use:       "bond-denom",
					Short:     "Show the chain's native gas/staking denom",
				},
				{
					RpcMethod: "DreamDenom",
					Use:       "dream-denom",
					Short:     "Show the chain's native DREAM denom",
				},
			},
		},
	}
}

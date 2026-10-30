package futarchy

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/futarchy/types"
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
					RpcMethod: "ListMarket",
					Use:       "list-market",
					Short:     "List all market",
				},
				{
					RpcMethod:      "GetMarket",
					Use:            "get-market [id]",
					Short:          "Gets a market",
					Alias:          []string{"show-market"},
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "index"}},
				},
				{
					// Flags (--market-id, --is-yes, --amount) are kept: e2e scripts use them.
					RpcMethod: "GetMarketPrice",
					Use:       "get-market-price",
					Short:     "Quote the shares and price a trade of --amount would get",
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
					RpcMethod:      "CreateMarket",
					Use:            "create-market [symbol] [initial-liquidity] [question] [end-block]",
					Short:          "Send a create_market tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "symbol"}, {ProtoField: "initial_liquidity"}, {ProtoField: "question"}, {ProtoField: "end_block"}},
				},
				{
					RpcMethod:      "Trade",
					Use:            "trade [market-id] [is-yes] [amount-in]",
					Short:          "Send a trade tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "market_id"}, {ProtoField: "is_yes"}, {ProtoField: "amount_in"}},
				},
				{
					RpcMethod:      "Redeem",
					Use:            "redeem [market-id]",
					Short:          "Send a redeem tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "market_id"}},
				},
				{
					// Authority-gated (gov); normally submitted inside a proposal.
					RpcMethod: "CancelMarket",
					Use:       "cancel-market",
					Short:     "Cancel an active market (gov authority); holders redeem at the LMSR price",
				},
				{
					// --market-id flag kept: e2e scripts use it.
					RpcMethod: "WithdrawLiquidity",
					Use:       "withdraw-liquidity",
					Short:     "Withdraw the creator's unpaid LMSR residual from a resolved market",
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

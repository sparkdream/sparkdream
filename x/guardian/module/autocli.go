package guardian

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/guardian/types"
)

// AutoCLIOptions wires the AllowedMsgs query into the CLI. Guardian's
// MsgExec takes a google.protobuf.Any inner and cannot be expressed via
// autocli; proposals invoking MsgExec are built as JSON and submitted
// through `tx gov submit-proposal`. There is no Tx surface here.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "AllowedMsgs",
					Use:       "allowed-msgs",
					Short:     "List the inner msg type URLs guardian.MsgExec will route",
				},
			},
		},
	}
}

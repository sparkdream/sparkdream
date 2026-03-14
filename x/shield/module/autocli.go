package shield

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/shield/types"
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
					RpcMethod:      "ShieldedOp",
					Use:            "shielded-op [message-type-url]",
					Short:          "Query a registered shielded operation",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "message_type_url"}},
				},
				{
					RpcMethod: "ShieldedOps",
					Use:       "shielded-ops",
					Short:     "List all registered shielded operations",
				},
				{
					RpcMethod: "ModuleBalance",
					Use:       "module-balance",
					Short:     "Query shield module account balance",
				},
				{
					RpcMethod:      "NullifierUsed",
					Use:            "nullifier-used [domain] [scope] [nullifier-hex]",
					Short:          "Check if a nullifier has been used",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "domain"}, {ProtoField: "scope"}, {ProtoField: "nullifier_hex"}},
				},
				{
					RpcMethod:      "DayFunding",
					Use:            "day-funding [day]",
					Short:          "Query funding amount for a given day",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "day"}},
				},
				{
					RpcMethod: "ShieldEpoch",
					Use:       "shield-epoch",
					Short:     "Query current shield epoch state",
				},
				{
					RpcMethod: "PendingOps",
					Use:       "pending-ops",
					Short:     "Query pending shielded operations",
				},
				{
					RpcMethod: "PendingOpCount",
					Use:       "pending-op-count",
					Short:     "Query pending operation count",
				},
				{
					RpcMethod: "TLEMasterPublicKey",
					Use:       "tle-master-public-key",
					Short:     "Query TLE master public key",
				},
				{
					RpcMethod: "TLEKeySet",
					Use:       "tle-key-set",
					Short:     "Query the full TLE key set",
				},
				{
					RpcMethod:      "VerificationKey",
					Use:            "verification-key [circuit-id]",
					Short:          "Query a ZK verification key by circuit ID",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "circuit_id"}},
				},
				{
					RpcMethod:      "TLEMissCount",
					Use:            "tle-miss-count [validator-address]",
					Short:          "Query a validator's TLE miss count",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "validator_address"}},
				},
				{
					RpcMethod:      "DecryptionShares",
					Use:            "decryption-shares [epoch]",
					Short:          "Query decryption shares for an epoch",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "epoch"}},
				},
				{
					RpcMethod:      "IdentityRateLimit",
					Use:            "identity-rate-limit [rate-limit-nullifier-hex]",
					Short:          "Query remaining rate limit for a rate-limit nullifier",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "rate_limit_nullifier_hex"}},
				},
				{
					RpcMethod: "DKGState",
					Use:       "dkg-state",
					Short:     "Query the current DKG ceremony state",
				},
				{
					RpcMethod: "DKGContributions",
					Use:       "dkg-contributions",
					Short:     "List all DKG contributions for the current round",
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
					RpcMethod: "ShieldedExec",
					Skip:      true, // custom CLI handles Any-typed inner message
				},
				{
					RpcMethod: "TriggerDKG",
					Skip:      true, // authority gated
				},
				{
					RpcMethod: "RegisterShieldedOp",
					Skip:      true, // authority gated
				},
				{
					RpcMethod: "DeregisterShieldedOp",
					Skip:      true, // authority gated
				},
			},
		},
	}
}

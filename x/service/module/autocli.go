package service

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"sparkdream/x/service/types"
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
					RpcMethod:      "Operator",
					Use:            "operator [address] [service-type]",
					Short:          "Query operator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}, {ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "ServiceType",
					Use:            "service-type [service-type]",
					Short:          "Query service-type",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "ServiceTypes",
					Use:            "service-types ",
					Short:          "Query service-types",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "Operators",
					Use:            "operators ",
					Short:          "Query operators",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{},
				},
				{
					RpcMethod:      "OperatorsByController",
					Use:            "operators-by-controller [controller]",
					Short:          "Query operators-by-controller",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "controller"}},
				},
				{
					RpcMethod:      "OperatorsByServiceType",
					Use:            "operators-by-service-type [service-type]",
					Short:          "Query operators-by-service-type",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "Report",
					Use:            "report [report-id]",
					Short:          "Query report",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "report_id"}},
				},
				{
					RpcMethod:      "ReportsByOperator",
					Use:            "reports-by-operator [operator-address] [service-type]",
					Short:          "Query reports-by-operator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator_address"}, {ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "OperatorReputationSnapshot",
					Use:            "operator-reputation-snapshot [address]",
					Short:          "Query operator-reputation-snapshot",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "address"}},
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
					RpcMethod:      "RegisterOperator",
					Use:            "register-operator [service-type] [controller] [bond-amount] [metadata]",
					Short:          "Send a register-operator tx. bond-amount is a bare Int in the chain's bond denom.",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}, {ProtoField: "controller"}, {ProtoField: "bond_amount"}, {ProtoField: "metadata", Varargs: true}},
				},
				{
					RpcMethod:      "UpdateMetadata",
					Use:            "update-metadata [service-type] [new-metadata]",
					Short:          "Send a update-metadata tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}, {ProtoField: "new_metadata", Varargs: true}},
				},
				{
					RpcMethod:      "UnbondOperator",
					Use:            "unbond-operator [service-type]",
					Short:          "Send a unbond-operator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "ClaimUnbondedBond",
					Use:            "claim-unbonded-bond [service-type]",
					Short:          "Send a claim-unbonded-bond tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}},
				},
				{
					RpcMethod:      "TopUpBond",
					Use:            "top-up-bond [service-type] [additional-bond-amount]",
					Short:          "Send a top-up-bond tx. additional-bond-amount is a bare Int in the chain's bond denom.",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}, {ProtoField: "additional_bond_amount"}},
				},
				{
					RpcMethod:      "ReportOperator",
					Use:            "report-operator [operator] [service-type] [reason]",
					Short:          "Send a report-operator tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "service_type"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "ResolveReport",
					Use:            "resolve-report [report-id] [verdict] [slash-bps]",
					Short:          "Send a resolve-report tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "report_id"}, {ProtoField: "verdict"}, {ProtoField: "slash_bps"}},
				},
				{
					RpcMethod:      "ContestSlash",
					Use:            "contest-slash [service-type] [report-id]",
					Short:          "Send a contest-slash tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "service_type"}, {ProtoField: "report_id"}},
				},
				{
					RpcMethod:      "ResolveReportByJury",
					Use:            "resolve-report-by-jury [report-id] [verdict] [slash-bps] [dissolve]",
					Short:          "Send a resolve-report-by-jury tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "report_id"}, {ProtoField: "verdict"}, {ProtoField: "slash_bps"}, {ProtoField: "dissolve"}},
				},
				{
					RpcMethod:      "OpenControllerTransferCase",
					Use:            "open-controller-transfer-case [operator] [service-type] [proposed-new-controller] [reason]",
					Short:          "Send a open-controller-transfer-case tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "operator"}, {ProtoField: "service_type"}, {ProtoField: "proposed_new_controller"}, {ProtoField: "reason"}},
				},
				{
					RpcMethod:      "FinalizeControllerTransfer",
					Use:            "finalize-controller-transfer [jury-case-id] [verdict] [new-controller]",
					Short:          "Send a finalize-controller-transfer tx",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "jury_case_id"}, {ProtoField: "verdict"}, {ProtoField: "new_controller"}},
				},
				{
					// UpdateServiceTypeConfig is gov-gated; submit via
					// `tx gov submit-proposal` rather than direct CLI.
					RpcMethod: "UpdateServiceTypeConfig",
					Skip:      true,
				},
			},
		},
	}
}

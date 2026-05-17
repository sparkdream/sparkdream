package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateServiceTypeConfig{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFinalizeControllerTransfer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgOpenControllerTransferCase{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveReportByJury{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgContestSlash{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReportOperator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTopUpBond{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgClaimUnbondedBond{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnbondOperator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateMetadata{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterOperator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}

package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnhideContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgHideContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealHide{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBuy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDelist{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateListing{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgList{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFreezeTokenMetadata{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateToken{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevoke{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBurn{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetReceivePolicy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelOutgoing{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRejectIncoming{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptIncoming{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransfer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPublicMint{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMint{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeleteClass{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelClassOwner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptClassOwner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgProposeClassOwner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCloseMinting{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetMaxSupply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetMinters{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetMintPolicy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFreezeClassMetadata{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateClass{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateClass{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}

package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetSeekingEndorsement{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgEndorseCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealHide{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgHideContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFlagContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDownvoteContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpvoteContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSponsorCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelSponsorshipRequest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRequestSponsorship{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgChallengeReview{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRateCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnregisterCurator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterCurator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateCollaboratorRole{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveCollaborator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAddCollaborator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReorderItem{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveItems{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveItem{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateItem{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAddItems{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAddItem{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeleteCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateCollection{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPinCollection{},
	)

	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}

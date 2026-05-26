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
		&MsgEscalateChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveEscalatedChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitArbiterHash{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgChallengeVerification{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVerifyContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRequestReputationAttestation{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgConfirmIdentityLink{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnlinkIdentity{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLinkIdentity{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgModerateContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAttestOutbound{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFederateContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitFederatedContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterBridge{},
	)

	// MsgUpdatePeerController / MsgResyncBridgeCount / MsgPruneOrphanBindings
	// added in Phase 1 of the federation→service migration.
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdatePeerController{},
	)
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResyncBridgeCount{},
	)
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPruneOrphanBindings{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdatePeerPolicy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResumePeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSuspendPeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemovePeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterPeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}

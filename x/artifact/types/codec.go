package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelAuction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSettleAuction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBid{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgStartAuction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptOffer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelOffer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMakeOffer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateUnitListing{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgForceBuy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetHarbergerPrice{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDetach{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgNest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgEndRental{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptRental{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelRentalOffer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgOfferRental{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCraft{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveCraftRecipe{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAddCraftRecipe{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgClaimEditionRewards{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFundEdition{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBuyUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDelistUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgListUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevokeUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBurnUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransferUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMintUnits{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateEdition{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetDisplayedMedia{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRejectMedia{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptMedia{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgProposeMedia{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevealSeed{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReveal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLockRoyalty{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRedeem{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRenew{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgIssue{},
	)

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

package ante

import (
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ProposalFeeDecorator enforces the minimum proposal fee for MsgSubmitProposal.
// All permission and term-expiration checks are handled in the message server.
type ProposalFeeDecorator struct {
	commonsKeeper keeper.Keeper
}

func NewProposalFeeDecorator(ck keeper.Keeper) ProposalFeeDecorator {
	return ProposalFeeDecorator{
		commonsKeeper: ck,
	}
}

func (ad ProposalFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	var cachedMinFee sdk.Coins
	paramsLoaded := false

	for _, msg := range tx.GetMsgs() {
		submitMsg, ok := msg.(*types.MsgSubmitProposal)
		if !ok {
			continue
		}

		// Determine if this proposal requires a fee
		requiresFee := false
		for _, anyMsg := range submitMsg.Messages {
			var sdkMsg sdk.Msg
			if err := ad.commonsKeeper.Codec().UnpackAny(anyMsg, &sdkMsg); err != nil {
				continue
			}
			typeURL := sdk.MsgTypeURL(sdkMsg)

			// Emergency actions are fee-exempt
			if typeURL != "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal" &&
				typeURL != "/sparkdream.commons.v1.MsgVetoGroupProposals" {
				requiresFee = true
			}
		}

		if requiresFee {
			if !paramsLoaded {
				params, err := ad.commonsKeeper.Params.Get(ctx)
				if err != nil {
					return ctx, err
				}
				fee, err := sdk.ParseCoinsNormalized(params.ProposalFee)
				if err != nil {
					return ctx, err
				}
				cachedMinFee = fee
				paramsLoaded = true
			}

			if !cachedMinFee.IsZero() {
				feeTx, ok := tx.(sdk.FeeTx)
				if !ok {
					return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
				}
				if !feeTx.GetFee().IsAllGTE(cachedMinFee) {
					return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "Commons Council requires min fee %s", cachedMinFee)
				}
			}
		}
	}

	return next(ctx, tx, simulate)
}

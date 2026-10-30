package ante

import (
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
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
	// Every fee-requiring proposal in the tx pays the fee: N proposals in
	// one tx need N times proposal_fee.
	feeCount := int64(0)

	for _, msg := range tx.GetMsgs() {
		submitMsg, ok := msg.(*types.MsgSubmitProposal)
		if !ok {
			continue
		}

		// Determine if this proposal requires a fee. Signaling proposals
		// (no inner messages) pay the fee too — they still consume proposal
		// storage and voter attention, so making them free would invite
		// cheap spam.
		requiresFee := len(submitMsg.Messages) == 0
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
			feeCount++
		}
	}

	if feeCount > 0 {
		params, err := ad.commonsKeeper.Params.Get(ctx)
		if err != nil {
			return ctx, err
		}
		fee, err := sdk.ParseCoinsNormalized(params.ProposalFee)
		if err != nil {
			return ctx, err
		}
		if !fee.IsZero() {
			minFee := fee.MulInt(math.NewInt(feeCount))
			feeTx, ok := tx.(sdk.FeeTx)
			if !ok {
				return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
			}
			if !feeTx.GetFee().IsAllGTE(minFee) {
				return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee,
					"Commons Council requires min fee %s for %d proposal(s)", minFee, feeCount)
			}
		}
	}

	return next(ctx, tx, simulate)
}

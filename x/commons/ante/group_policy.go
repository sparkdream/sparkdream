package ante

import (
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ProposalFeeDecorator floors the tx fee of every top-level MsgSubmitProposal
// that owes the proposal fee (N such proposals need N times proposal_fee in
// tx fees) and records what it floored as per-proposer credits on the
// context, so the handler charges only the submissions that reached it
// another way (nested in an x/vault proposal, for instance). See
// x/commons/keeper/proposal_fee.go. All permission and term-expiration
// checks are handled in the message server.
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
	// one tx need N times proposal_fee. Signaling proposals (no inner
	// messages) pay too; proposals made only of emergency actions are
	// exempt (keeper.ProposalRequiresFee).
	feeCount := int64(0)
	credits := map[string]int{}

	for _, msg := range tx.GetMsgs() {
		submitMsg, ok := msg.(*types.MsgSubmitProposal)
		if !ok || !ad.commonsKeeper.ProposalRequiresFee(submitMsg) {
			continue
		}
		feeCount++
		credits[submitMsg.Proposer]++
	}

	if feeCount > 0 {
		fee, err := ad.commonsKeeper.ProposalFee(ctx)
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
		// The floored submissions are paid: the handler must not charge
		// them again.
		ctx = keeper.WithProposalFeeCredits(ctx, credits)
	}

	return next(ctx, tx, simulate)
}

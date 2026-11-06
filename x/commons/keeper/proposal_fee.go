package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Proposal fee (spec §6.1). Params.proposal_fee is owed once per
// fee-requiring MsgSubmitProposal, whichever way the message reaches the
// handler:
//
//   - A top-level MsgSubmitProposal is floored by the ProposalFeeDecorator
//     ante: the tx fee must cover the fee once per such message, so the
//     mempool turns unfunded spam away before any state is read. The ante
//     records what it floored as per-proposer credits on the context.
//   - The handler consumes one credit for its proposer when there is one,
//     and otherwise transfers the fee itself from the proposer to the fee
//     collector (the same destination as a tx fee). That is the path of a
//     MsgSubmitProposal nested in an in-chain executor (an x/vault proposal,
//     a commons proposal executing as a policy), which skips the ante.
//
// A credit is consumed at most once and the ante grants exactly one per
// floored message, so the two paths never both charge the same proposal.
// A handler-side charge is reverted with the tx when the submission fails
// (baseapp discards the handler's writes); an ante-floored fee is kept like
// any tx fee.

// proposalFeeCreditsKey is the context key of the ante's credits.
type proposalFeeCreditsKey struct{}

// proposalFeeCredits is a shared, mutable per-proposer count of the
// fee-requiring top-level MsgSubmitProposal the ante floored in this tx. It
// is a pointer so handler-side consumption is visible across messages and
// through CacheContext (an executor's child context keeps the parent's
// values).
type proposalFeeCredits struct {
	byProposer map[string]int
}

// WithProposalFeeCredits records the per-proposer count of top-level
// fee-requiring MsgSubmitProposal the ante floored. Called by the
// ProposalFeeDecorator; handlers consume the credits.
func WithProposalFeeCredits(ctx sdk.Context, byProposer map[string]int) sdk.Context {
	credits := &proposalFeeCredits{byProposer: make(map[string]int, len(byProposer))}
	for proposer, n := range byProposer {
		credits.byProposer[proposer] = n
	}
	return ctx.WithValue(proposalFeeCreditsKey{}, credits)
}

// consumeProposalFeeCredit uses one of the proposer's ante credits and
// reports whether there was one.
func consumeProposalFeeCredit(ctx context.Context, proposer string) bool {
	credits, ok := ctx.Value(proposalFeeCreditsKey{}).(*proposalFeeCredits)
	if !ok || credits == nil || credits.byProposer[proposer] <= 0 {
		return false
	}
	credits.byProposer[proposer]--
	return true
}

// ProposalFeeExemptMessages are the emergency actions whose proposals pay
// no fee when every inner message is one of them.
var ProposalFeeExemptMessages = map[string]bool{
	"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal": true,
	"/sparkdream.commons.v1.MsgVetoGroupProposals":         true,
}

// ProposalRequiresFee reports whether a MsgSubmitProposal owes the proposal
// fee: a signaling proposal (no inner messages) does, since it still takes
// proposal storage and voter attention, and so does any proposal carrying
// a message outside ProposalFeeExemptMessages. An inner message that does
// not unpack is ignored here; the handler refuses it later.
func (k Keeper) ProposalRequiresFee(msg *types.MsgSubmitProposal) bool {
	if len(msg.Messages) == 0 {
		return true
	}
	for _, anyMsg := range msg.Messages {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			continue
		}
		if !ProposalFeeExemptMessages[sdk.MsgTypeURL(sdkMsg)] {
			return true
		}
	}
	return false
}

// ProposalFee returns Params.proposal_fee as coins.
func (k Keeper) ProposalFee(ctx context.Context) (sdk.Coins, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	return sdk.ParseCoinsNormalized(params.ProposalFee)
}

// chargeProposalFee collects the proposal fee for msg unless the ante
// already floored it (a credit for the proposer) or the proposal is exempt.
// The fee goes from the proposer to the fee collector, where a tx fee goes.
// A group policy paying it (a council proposal executing as the policy that
// submits a proposal to another body) spends treasury, so the fee passes
// the spend gates and debits the per-epoch budget like any other policy
// disbursement (COM-202).
func (k Keeper) chargeProposalFee(ctx sdk.Context, msg *types.MsgSubmitProposal) error {
	if !k.ProposalRequiresFee(msg) || consumeProposalFeeCredit(ctx, msg.Proposer) {
		return nil
	}
	fee, err := k.ProposalFee(ctx)
	if err != nil {
		return err
	}
	if fee.IsZero() {
		return nil
	}
	if k.bankKeeper == nil {
		return errorsmod.Wrap(sdkerrors.ErrLogic, "bank keeper not wired into commons keeper")
	}
	proposer, err := k.addressCodec.StringToBytes(msg.Proposer)
	if err != nil {
		return errorsmod.Wrap(err, "invalid proposer address")
	}
	if k.IsGroupPolicyAddress(ctx, msg.Proposer) {
		if err := k.CheckSpendPreconditions(ctx, msg.Proposer, fee); err != nil {
			return errorsmod.Wrapf(types.ErrInsufficientProposalFee, "%s cannot spend %s: %v", msg.Proposer, fee, err)
		}
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, proposer, authtypes.FeeCollectorName, fee); err != nil {
		return errorsmod.Wrapf(types.ErrInsufficientProposalFee, "%s owes %s: %v", msg.Proposer, fee, err)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"proposal_fee_charged",
		sdk.NewAttribute("proposer", msg.Proposer),
		sdk.NewAttribute("fee", fee.String()),
	))
	return nil
}

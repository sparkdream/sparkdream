package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	// 1. ADD IMPORT
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	grouperrors "github.com/cosmos/cosmos-sdk/x/group/errors"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
)

const (
	// 2. UPDATE CONSTANT: Use MsgSend for the signal
	msgSendTypeURL                 = "/cosmos.bank.v1beta1.MsgSend"
	msgSpendFromCommonsTypeURL     = "/sparkdream.split.v1.MsgSpendFromCommons"
	msgUpdateGroupMembersTypeURL   = "/cosmos.group.v1.MsgUpdateGroupMembers"
	msgUpdateDecisionPolicyTypeURL = "/cosmos.group.v1.MsgUpdateGroupPolicyDecisionPolicy"
)

// GroupPolicyDecorator checks if a MsgSubmitProposal is allowed for the specific Group Policy
type GroupPolicyDecorator struct {
	groupKeeper groupkeeper.Keeper
}

func NewGroupPolicyDecorator(gk groupkeeper.Keeper) GroupPolicyDecorator {
	return GroupPolicyDecorator{groupKeeper: gk}
}

func (ad GroupPolicyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		// We only care about Group Proposals
		submitMsg, ok := msg.(*group.MsgSubmitProposal)
		if !ok {
			continue
		}

		// 1. Get Policy Info
		policyInfo, err := ad.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: submitMsg.GroupPolicyAddress,
		})
		if err != nil {
			return ctx, errorsmod.Wrap(err, "could not get group policy info")
		}

		// 2. Check Inner Messages
		msgs, err := submitMsg.GetMsgs()
		if err != nil {
			return ctx, errorsmod.Wrap(err, "failed to get msgs from proposal")
		}

		// 3. Check the policy metadata
		if policyInfo.Info.Metadata != "standard" && policyInfo.Info.Metadata != "veto" {
			return ctx, errorsmod.Wrap(err, "invalid policy metadata")
		}

		// 4. SPAM PROTECTION: Enforce Minimum Fee
		requiredFee := sdk.NewCoins(sdk.NewInt64Coin("uspark", 5000000))

		// Get the fee payer's provided fee from the Tx
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
		}

		providedFee := feeTx.GetFee()

		// Check if provided fee >= required fee
		if !providedFee.IsAllGTE(requiredFee) {
			return ctx, errorsmod.Wrapf(
				sdkerrors.ErrInsufficientFee,
				"Commons Council proposals require a min fee of %s, got %s",
				requiredFee, providedFee,
			)
		}

		// 5. Apply Allowlist Logic
		switch policyInfo.Info.Metadata {
		case "standard":
			for _, innerMsg := range msgs {
				typeURL := sdk.MsgTypeURL(innerMsg)
				if typeURL != msgSpendFromCommonsTypeURL &&
					typeURL != msgUpdateGroupMembersTypeURL &&
					typeURL != msgUpdateDecisionPolicyTypeURL {
					return ctx, errorsmod.Wrapf(
						grouperrors.ErrUnauthorized,
						"msg type %s not allowed for 'standard' policy (only SpendFromCommons and UpdateGroupMembers allowed)",
						typeURL,
					)
				}
			}
		case "veto":
			for _, innerMsg := range msgs {
				// 3. CHECK MSG TYPE
				if sdk.MsgTypeURL(innerMsg) != msgSendTypeURL {
					return ctx, errorsmod.Wrapf(
						grouperrors.ErrUnauthorized,
						"msg type %s not allowed for 'veto' policy (only Self-Send signals allowed)",
						sdk.MsgTypeURL(innerMsg),
					)
				}

				// 4. ENFORCE LOOPBACK (From == To)
				// This ensures the Veto Policy isn't spending money, just signaling.
				sendMsg, ok := innerMsg.(*banktypes.MsgSend)
				if !ok {
					return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidType, "could not cast to MsgSend")
				}
				if sendMsg.FromAddress != sendMsg.ToAddress {
					return ctx, errorsmod.Wrap(
						grouperrors.ErrUnauthorized,
						"Veto Policy can only send to itself (Loopback Signal)",
					)
				}
			}
		}
	}

	// Continue to next decorator
	return next(ctx, tx, simulate)
}

package ante

import (
	"sparkdream/x/commons/keeper"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	grouperrors "github.com/cosmos/cosmos-sdk/x/group/errors"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
)

const (
	msgSendTypeURL                    = "/cosmos.bank.v1beta1.MsgSend"
	msgSpendFromCommonsTypeURL        = "/sparkdream.commons.v1.MsgSpendFromCommons"
	msgEmergencyCancelProposalTypeURL = "/sparkdream.commons.v1.MsgEmergencyCancelProposal"
	msgUpdateGroupMembersTypeURL      = "/cosmos.group.v1.MsgUpdateGroupMembers"
	msgUpdateDecisionPolicyTypeURL    = "/cosmos.group.v1.MsgUpdateGroupPolicyDecisionPolicy"
	msgResolveNameDisputeTypeURL      = "/sparkdream.name.v1.MsgResolveDispute"
)

// GroupPolicyDecorator checks if a MsgSubmitProposal is allowed for the specific Group Policy
type GroupPolicyDecorator struct {
	groupKeeper   groupkeeper.Keeper
	commonsKeeper keeper.Keeper
}

func NewGroupPolicyDecorator(gk groupkeeper.Keeper, ck keeper.Keeper) GroupPolicyDecorator {
	return GroupPolicyDecorator{
		groupKeeper:   gk,
		commonsKeeper: ck,
	}
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

		// 4. Get Stored Council Params
		params, err := ad.commonsKeeper.Params.Get(ctx)
		if err != nil {
			return ctx, errorsmod.Wrap(err, "failed to get params")
		}

		// 5. SPAM PROTECTION: Enforce Minimum Fee (only for Standard policy)
		if policyInfo.Info.Metadata == "standard" {
			// Parse the string from params into Coins
			// If param is empty or invalid, this returns empty coins (0 fee), which is safe fallback
			requiredFee, _ := sdk.ParseCoinsNormalized(params.CommonsCouncilFee)

			if !requiredFee.IsZero() {
				feeTx, ok := tx.(sdk.FeeTx)
				if !ok {
					return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
				}

				providedFee := feeTx.GetFee()
				if !providedFee.IsAllGTE(requiredFee) {
					return ctx, errorsmod.Wrapf(
						sdkerrors.ErrInsufficientFee,
						"Commons Council proposals require a min fee of %s, got %s",
						requiredFee, providedFee,
					)
				}
			}
		}

		// 6. Apply Allowlist Logic
		switch policyInfo.Info.Metadata {
		case "standard":
			for _, innerMsg := range msgs {
				typeURL := sdk.MsgTypeURL(innerMsg)
				if typeURL != msgSpendFromCommonsTypeURL &&
					typeURL != msgUpdateGroupMembersTypeURL &&
					typeURL != msgUpdateDecisionPolicyTypeURL &&
					typeURL != msgResolveNameDisputeTypeURL {
					return ctx, errorsmod.Wrapf(
						grouperrors.ErrUnauthorized,
						"msg type %s not allowed for 'standard' policy (only SpendFromCommons and UpdateGroupMembers allowed)",
						typeURL,
					)
				}
			}
		case "veto":
			for _, innerMsg := range msgs {
				typeURL := sdk.MsgTypeURL(innerMsg)

				// OPTION A: Executive Order (Emergency Kill)
				if typeURL == msgEmergencyCancelProposalTypeURL {
					continue // Allowed
				}

				// OPTION B: Social Signal (Loopback MsgSend)
				if typeURL == msgSendTypeURL {
					// Enforce Loopback (From == To) to prevent spending
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
					continue // Allowed
				}

				// If it's neither, reject it
				return ctx, errorsmod.Wrapf(
					grouperrors.ErrUnauthorized,
					"msg type %s not allowed for 'veto' policy",
					typeURL,
				)
			}
		}
	}

	// Continue to next decorator
	return next(ctx, tx, simulate)
}

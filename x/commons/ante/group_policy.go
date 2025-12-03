package ante

import (
	"context"
	"sparkdream/x/commons/keeper"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	grouperrors "github.com/cosmos/cosmos-sdk/x/group/errors"
)

// Define the interface for the method we need
type GroupKeeper interface {
	GroupPolicyInfo(ctx context.Context, request *group.QueryGroupPolicyInfoRequest) (*group.QueryGroupPolicyInfoResponse, error)
}

// GroupPolicyDecorator checks if a MsgSubmitProposal is allowed for the specific Group Policy
type GroupPolicyDecorator struct {
	groupKeeper   GroupKeeper
	commonsKeeper keeper.Keeper
}

func NewGroupPolicyDecorator(gk GroupKeeper, ck keeper.Keeper) GroupPolicyDecorator {
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
		_, err := ad.groupKeeper.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{
			Address: submitMsg.GroupPolicyAddress,
		})
		if err != nil {
			return ctx, errorsmod.Wrap(err, "could not get group policy info")
		}

		// 2. Get Stored Permissions
		perms, err := ad.commonsKeeper.PolicyPermissions.Get(ctx, submitMsg.GroupPolicyAddress)

		// SECURITY: If no permissions are found in the DB, we REJECT.
		if err != nil {
			return ctx, errorsmod.Wrapf(
				grouperrors.ErrUnauthorized,
				"no policy permissions found for address %s",
				submitMsg.GroupPolicyAddress,
			)
		}

		// 3. Check Inner Messages
		msgs, err := submitMsg.GetMsgs()
		if err != nil {
			return ctx, errorsmod.Wrap(err, "failed to get msgs from proposal")
		}

		for _, innerMsg := range msgs {
			typeURL := sdk.MsgTypeURL(innerMsg)

			// 3a. Check against the DB Allowlist
			isAllowed := false
			for _, allowedURL := range perms.AllowedMessages {
				if typeURL == allowedURL {
					isAllowed = true
					break
				}
			}

			if !isAllowed {
				return ctx, errorsmod.Wrapf(
					grouperrors.ErrUnauthorized,
					"msg type %s is not in the allowlist for policy %s",
					typeURL, submitMsg.GroupPolicyAddress,
				)
			}

			// 3b. Special Logic Checks (e.g. Loopback for MsgSend)
			// Even if MsgSend is allowed in the DB, we enforce it must be a self-send.
			if typeURL == "/cosmos.bank.v1beta1.MsgSend" {
				sendMsg, ok := innerMsg.(*banktypes.MsgSend)
				if !ok {
					return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidType, "could not cast to MsgSend")
				}
				if sendMsg.FromAddress != sendMsg.ToAddress {
					return ctx, errorsmod.Wrap(
						grouperrors.ErrUnauthorized,
						"Commons Council policies can only send funds to themselves (Loopback Signal)",
					)
				}
			}
		}

		// 4. SPAM PROTECTION: Enforce Minimum Fee
		// EXEMPTION LOGIC:
		// If a policy is allowed to perform "MsgEmergencyCancelProposal", we consider it a Veto Policy
		// and exempt it from fees to ensure emergency actions are never blocked by lack of funds.
		isVetoPolicy := false
		for _, allowedMsg := range perms.AllowedMessages {
			if allowedMsg == "/sparkdream.commons.v1.MsgEmergencyCancelProposal" {
				isVetoPolicy = true
				break
			}
		}

		if !isVetoPolicy {
			// Fee enforcement for non-Veto policies
			params, err := ad.commonsKeeper.Params.Get(ctx)
			if err != nil {
				return ctx, errorsmod.Wrap(err, "failed to get commons params")
			}

			requiredFee, err := sdk.ParseCoinsNormalized(params.CommonsCouncilFee)
			if err != nil {
				return ctx, errorsmod.Wrap(err, "invalid commons council fee param")
			}

			if !requiredFee.IsZero() {
				feeTx, ok := tx.(sdk.FeeTx)
				if !ok {
					return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
				}
				if !feeTx.GetFee().IsAllGTE(requiredFee) {
					return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "Commons Council proposals require min fee %s", requiredFee)
				}
			}
		}
	}

	return next(ctx, tx, simulate)
}

package ante

import (
	"context"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	grouperrors "github.com/cosmos/cosmos-sdk/x/group/errors"
)

// GroupKeeper defines the expected interface for the x/group module.
// We added Proposal() to fetch proposal details for MsgExec checks.
type GroupKeeper interface {
	GroupPolicyInfo(ctx context.Context, request *group.QueryGroupPolicyInfoRequest) (*group.QueryGroupPolicyInfoResponse, error)
	Proposal(ctx context.Context, request *group.QueryProposalRequest) (*group.QueryProposalResponse, error)
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
	var cachedMinFee sdk.Coins
	paramsLoaded := false

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {

		// ==========================================================
		// CASE 1: NEW PROPOSAL (MsgSubmitProposal)
		// ==========================================================
		case *group.MsgSubmitProposal:
			// 1. Basic Validation
			if _, err := sdk.AccAddressFromBech32(m.GroupPolicyAddress); err != nil {
				return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid group policy address")
			}

			// 2. Fetch Extended Group (Term Limits)
			// We perform the O(1) Index lookup to see if this is a regulated Council.
			groupName, err := ad.commonsKeeper.PolicyToName.Get(ctx, m.GroupPolicyAddress)
			var extendedGroup types.ExtendedGroup
			isRegulated := false

			if err == nil {
				// It's a registered council, fetch the full state
				extendedGroup, err = ad.commonsKeeper.ExtendedGroup.Get(ctx, groupName)
				if err == nil {
					isRegulated = true
				}
			}

			// 3. CHECK EXPIRATION (The "Lame Duck" Check)
			if isRegulated {
				// If current time > expiration, the group is FROZEN.
				if ctx.BlockTime().Unix() > extendedGroup.CurrentTermExpiration {
					// EXCEPTION: We allow them to propose a "RenewGroup" message to fix themselves.
					// We must peek inside the proposal to verify.
					msgs, err := m.GetMsgs()
					if err != nil {
						return ctx, err
					}

					for _, inner := range msgs {
						if sdk.MsgTypeURL(inner) != "/sparkdream.commons.v1.MsgRenewGroup" {
							return ctx, errorsmod.Wrapf(
								sdkerrors.ErrUnauthorized,
								"TERM EXPIRED: Group %s expired on %d. You can only submit MsgRenewGroup proposals.",
								extendedGroup.Index, extendedGroup.CurrentTermExpiration,
							)
						}
					}
				}
			}

			// 4. Permissions & Fees (Existing Logic)
			// Get Stored Permissions
			perms, err := ad.commonsKeeper.PolicyPermissions.Get(ctx, m.GroupPolicyAddress)
			if err != nil {
				return ctx, errorsmod.Wrapf(grouperrors.ErrUnauthorized, "no policy permissions found for %s", m.GroupPolicyAddress)
			}

			msgs, err := m.GetMsgs()
			if err != nil {
				return ctx, err
			}
			if len(msgs) == 0 {
				return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty proposal")
			}

			requiresFee := false

			for _, innerMsg := range msgs {
				typeURL := sdk.MsgTypeURL(innerMsg)

				// Allowlist Check
				isAllowed := false
				for _, allowedURL := range perms.AllowedMessages {
					if typeURL == allowedURL {
						isAllowed = true
						break
					}
				}
				if !isAllowed {
					return ctx, errorsmod.Wrapf(grouperrors.ErrUnauthorized, "msg %s not allowed for policy %s", typeURL, m.GroupPolicyAddress)
				}

				// Loopback Check
				if typeURL == "/cosmos.bank.v1beta1.MsgSend" {
					sendMsg, ok := innerMsg.(*banktypes.MsgSend)
					if !ok {
						return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidType, "could not cast MsgSend")
					}
					if sendMsg.FromAddress != sendMsg.ToAddress {
						return ctx, errorsmod.Wrap(grouperrors.ErrUnauthorized, "Council policies can only send funds to themselves (Loopback)")
					}
				}

				// Authz Check
				if typeURL == "/cosmos.authz.v1beta1.MsgExec" {
					return ctx, errorsmod.Wrap(grouperrors.ErrUnauthorized, "MsgExec (Authz) is forbidden")
				}

				// FEE EXEMPTIONS: Emergency Actions are free
				// We keep MsgDeleteGroup as paid because it is also a standard administrative action.
				if typeURL != "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal" &&
					typeURL != "/sparkdream.commons.v1.MsgVetoGroupProposals" {
					requiresFee = true
				}
			}

			// Fee Deduction
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

		// ==========================================================
		// CASE 2: EXECUTION (MsgExec)
		// ==========================================================
		// We must also gate execution to prevent "Zombie" groups from executing
		// pending proposals after their term has expired.
		case *group.MsgExec:
			// 1. Fetch the Proposal to find the Policy Address
			propRes, err := ad.groupKeeper.Proposal(ctx, &group.QueryProposalRequest{
				ProposalId: m.ProposalId,
			})
			if err != nil {
				return ctx, errorsmod.Wrap(err, "failed to fetch proposal for exec check")
			}

			policyAddr := propRes.Proposal.GroupPolicyAddress

			// 2. Fetch Extended Group
			groupName, err := ad.commonsKeeper.PolicyToName.Get(ctx, policyAddr)
			if err != nil {
				if errorsmod.IsOf(err, collections.ErrNotFound) {
					continue // Not a regulated group, let it pass
				}
				return ctx, err
			}

			extendedGroup, err := ad.commonsKeeper.ExtendedGroup.Get(ctx, groupName)
			if err != nil {
				return ctx, err
			}

			// 3. CHECK EXPIRATION
			if ctx.BlockTime().Unix() > extendedGroup.CurrentTermExpiration {
				// EXCEPTION: Allow "RenewGroup" execution
				msgs, err := propRes.Proposal.GetMsgs()
				if err != nil {
					return ctx, err
				}

				for _, inner := range msgs {
					if sdk.MsgTypeURL(inner) != "/sparkdream.commons.v1.MsgRenewGroup" {
						return ctx, errorsmod.Wrapf(
							sdkerrors.ErrUnauthorized,
							"TERM EXPIRED: Group %s expired on %d. Cannot execute pending proposal %d.",
							extendedGroup.Index, extendedGroup.CurrentTermExpiration, m.ProposalId,
						)
					}
				}
			}
		}
	}

	return next(ctx, tx, simulate)
}

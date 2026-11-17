package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// DelegableMsgTypeURLs are the messages an operator or scanner worker signs
// with an x/session key. None moves the signer's funds, and each closing
// power among them is capped per window.
func DelegableMsgTypeURLs() []string {
	return []string{
		sdk.MsgTypeURL(&MsgClaimRequest{}),
		sdk.MsgTypeURL(&MsgExtendClaim{}),
		sdk.MsgTypeURL(&MsgCompleteRequest{}),
		sdk.MsgTypeURL(&MsgDeclineRequest{}),
		sdk.MsgTypeURL(&MsgFlagRemoved{}),
	}
}

// NonDelegableMsgTypeURLs are the messages that may never be in the x/session
// allowlist: they move a member's funds, hold or return a council's, or are
// council and governance decisions.
func NonDelegableMsgTypeURLs() []string {
	return []string{
		sdk.MsgTypeURL(&MsgUpdateParams{}),
		sdk.MsgTypeURL(&MsgUpdateBackend{}),
		sdk.MsgTypeURL(&MsgUpdateOperationalParams{}),
		sdk.MsgTypeURL(&MsgRequestPermanentCopy{}),
		sdk.MsgTypeURL(&MsgAddSource{}),
		sdk.MsgTypeURL(&MsgCancelRequest{}),
		sdk.MsgTypeURL(&MsgDisputeCompletion{}),
		sdk.MsgTypeURL(&MsgVoidCompletion{}),
		sdk.MsgTypeURL(&MsgUnflagRemoved{}),
		sdk.MsgTypeURL(&MsgApproveOperator{}),
		sdk.MsgTypeURL(&MsgRemoveOperator{}),
		sdk.MsgTypeURL(&MsgApproveScanner{}),
		sdk.MsgTypeURL(&MsgRemoveScanner{}),
	}
}

// CouncilMsgTypeURLs are the messages a council signs through a proposal, so
// they belong in its AllowedMessages.
func CouncilMsgTypeURLs() []string {
	return []string{
		sdk.MsgTypeURL(&MsgApproveOperator{}),
		sdk.MsgTypeURL(&MsgRemoveOperator{}),
		sdk.MsgTypeURL(&MsgApproveScanner{}),
		sdk.MsgTypeURL(&MsgRemoveScanner{}),
		sdk.MsgTypeURL(&MsgVoidCompletion{}),
		sdk.MsgTypeURL(&MsgUnflagRemoved{}),
	}
}

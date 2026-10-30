package types

// Ante-only rules (cross-cutting theme 6). A few chain rules run only in
// the ante handler, so a message dispatched by an in-chain executor (x/vault
// proposals, x/session exec and ScheduledOneshot Exec, the ICA host) skips
// them. Executors keep off these message types instead:
//
//   - x/commons MsgSubmitProposal: the minimum proposal fee is charged by
//     commons' ProposalFeeDecorator.
//   - x/gnovm MsgRun / MsgCall / MsgAddPackage: the gnovm ante refuses a tx
//     that mixes VM messages with any other message.
//
// x/session forbids all of them in its allowlists, x/vault refuses
// MsgSubmitProposal and applies the gnovm no-mix rule to a proposal's
// messages, and the ICA host only runs app.ICAHostAllowedMsgs.

// CommonsSubmitProposalTypeURL is the commons proposal msg whose fee floor
// is enforced only in the ante.
const CommonsSubmitProposalTypeURL = "/sparkdream.commons.v1.MsgSubmitProposal"

// GnoVMMsgTypeURLs are the gnovm messages the gnovm ante refuses to mix
// with any other message in one tx.
var GnoVMMsgTypeURLs = []string{
	"/gnovm.gnovm.v1.MsgAddPackage",
	"/gnovm.gnovm.v1.MsgCall",
	"/gnovm.gnovm.v1.MsgRun",
}

// IsGnoVMMsgTypeURL reports whether url is a gnovm VM message.
func IsGnoVMMsgTypeURL(url string) bool {
	for _, u := range GnoVMMsgTypeURLs {
		if u == url {
			return true
		}
	}
	return false
}

// AnteOnlyRuleMsgTypeURLs returns every message type guarded by an
// ante-only rule.
func AnteOnlyRuleMsgTypeURLs() []string {
	return append([]string{CommonsSubmitProposalTypeURL}, GnoVMMsgTypeURLs...)
}

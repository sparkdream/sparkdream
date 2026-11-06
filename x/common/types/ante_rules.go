package types

// Ante-only rules (cross-cutting theme 6). A few chain rules run only in
// the ante handler, so a message dispatched by an in-chain executor (x/vault
// proposals, x/session exec and ScheduledOneshot Exec, the ICA host) skips
// them. Executors keep off these message types instead:
//
//   - x/commons MsgSubmitProposal: the ante floors the tx fee at N proposal
//     fees; the msg server charges the fee itself when no ante credit is
//     present (nested submissions), so this one is ante-floored, not
//     ante-only. x/session still keeps it off the allowlists.
//   - x/gnovm MsgRun / MsgCall / MsgAddPackage: the gnovm ante refuses a tx
//     that mixes VM messages with any other message.
//
// x/session forbids all of them in its allowlists, x/vault applies the
// gnovm no-mix rule to a proposal's messages (a vault-signed
// MsgSubmitProposal pays the fee from the vault address), and the ICA host
// only runs app.ICAHostAllowedMsgs.

// CommonsSubmitProposalTypeURL is the commons proposal msg whose fee floor
// is enforced in the ante (the handler charges nested submissions itself).
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

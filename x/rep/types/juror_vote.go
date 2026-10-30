package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// ValidateJurorVote is the domain of a juror vote: the verdict must take a
// side (VERDICT_UPHOLD_CHALLENGE or VERDICT_REJECT_CHALLENGE) and the
// confidence must lie in [0, 1]. Every jury case (initiative and content
// challenges, moderation appeals and the external cases other modules open)
// is decided on these two verdicts; PENDING and INCONCLUSIVE are outcomes the
// tally records, never votes.
func ValidateJurorVote(verdict Verdict, confidence math.LegacyDec) error {
	switch verdict {
	case Verdict_VERDICT_UPHOLD_CHALLENGE, Verdict_VERDICT_REJECT_CHALLENGE:
	default:
		return errorsmod.Wrapf(ErrInvalidJurorVote, "verdict %s is not a vote", verdict.String())
	}
	if confidence.IsNil() || confidence.IsNegative() || confidence.GT(math.LegacyOneDec()) {
		return errorsmod.Wrapf(ErrInvalidJurorVote, "confidence %s is outside [0, 1]", confidence.String())
	}
	return nil
}

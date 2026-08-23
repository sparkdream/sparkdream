package keeper

import (
	"fmt"

	"sparkdream/x/rep/types"
)

// Acceptance criteria: a pre-committed definition of done.
//
// The gap this closes is F5 — nothing in the happy path reads the deliverable.
// Completion turns on conviction, and the only mechanism that inspects the work
// is the challenge system, which previously gave a challenger nothing but a
// free-form reason string to argue from.
//
// Criteria are deliberately *not* a completion gate of their own. Making them
// one would need an electorate to judge every submission, and the two available
// electorates are both wrong for it: stakers are paid only on completion (so
// they are paid to pass the work), and a lot-drawn jury costs more in
// participation rewards than a STANDARD initiative's entire budget. Instead
// criteria arm the one actor whose incentives are already correct — the
// challenger, who stakes DREAM that burns if they are wrong — with a concrete,
// objective standard the author agreed to before starting.

// ValidateAcceptanceCriteria checks a proposed criteria set at initiative
// creation. Criteria are immutable afterwards, so this is the only gate.
func ValidateAcceptanceCriteria(criteria []types.VerificationCriteria) error {
	if len(criteria) == 0 {
		return nil
	}
	if len(criteria) > types.MaxAcceptanceCriteria {
		return fmt.Errorf("%w: %d criteria declared, max %d",
			types.ErrInvalidAcceptanceCriteria, len(criteria), types.MaxAcceptanceCriteria)
	}

	seen := make(map[string]struct{}, len(criteria))
	for i, c := range criteria {
		if c.Id == "" {
			return fmt.Errorf("%w: criterion %d has an empty id", types.ErrInvalidAcceptanceCriteria, i)
		}
		if len(c.Id) > types.MaxCriteriaIDLength {
			return fmt.Errorf("%w: criterion id %q exceeds %d characters",
				types.ErrInvalidAcceptanceCriteria, c.Id, types.MaxCriteriaIDLength)
		}
		if _, dup := seen[c.Id]; dup {
			// Duplicate ids would make a CriteriaVote ambiguous about which
			// item it answered, and a challenge citation ambiguous about which
			// it disputes.
			return fmt.Errorf("%w: duplicate criterion id %q", types.ErrInvalidAcceptanceCriteria, c.Id)
		}
		seen[c.Id] = struct{}{}

		if c.Question == "" {
			return fmt.Errorf("%w: criterion %q has no question", types.ErrInvalidAcceptanceCriteria, c.Id)
		}
		if len(c.Question) > types.MaxCriteriaQuestionLength {
			return fmt.Errorf("%w: criterion %q question exceeds %d characters",
				types.ErrInvalidAcceptanceCriteria, c.Id, types.MaxCriteriaQuestionLength)
		}
		if len(c.HowToVerify) > types.MaxCriteriaQuestionLength {
			return fmt.Errorf("%w: criterion %q how_to_verify exceeds %d characters",
				types.ErrInvalidAcceptanceCriteria, c.Id, types.MaxCriteriaQuestionLength)
		}
	}
	return nil
}

// HasAcceptanceCriterion reports whether an initiative declared the given id.
func HasAcceptanceCriterion(initiative types.Initiative, criteriaID string) bool {
	for _, c := range initiative.AcceptanceCriteria {
		if c.Id == criteriaID {
			return true
		}
	}
	return false
}

// ValidateCriteriaCitation checks a criterion id supplied by a challenger.
// An empty id is always allowed — citing a criterion is optional, and an
// initiative that declared none can only ever be challenged free-form.
func ValidateCriteriaCitation(initiative types.Initiative, criteriaID string) error {
	if criteriaID == "" {
		return nil
	}
	if len(initiative.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%w: initiative %d declared no acceptance criteria",
			types.ErrUnknownAcceptanceCriterion, initiative.Id)
	}
	if !HasAcceptanceCriterion(initiative, criteriaID) {
		return fmt.Errorf("%w: initiative %d has no criterion %q",
			types.ErrUnknownAcceptanceCriterion, initiative.Id, criteriaID)
	}
	return nil
}

// ValidateCriteriaVotes checks that a juror's per-item verdicts answer criteria
// the initiative actually declared. Without this the ids were free-form text
// that nothing resolved, which is what made the whole CriteriaVote feature
// decorative.
func ValidateCriteriaVotes(initiative types.Initiative, votes []*types.CriteriaVote) error {
	if len(votes) == 0 {
		return nil
	}
	if len(initiative.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%w: initiative %d declared no acceptance criteria",
			types.ErrUnknownAcceptanceCriterion, initiative.Id)
	}

	seen := make(map[string]struct{}, len(votes))
	for _, v := range votes {
		if v == nil {
			continue
		}
		if !HasAcceptanceCriterion(initiative, v.CriteriaId) {
			return fmt.Errorf("%w: initiative %d has no criterion %q",
				types.ErrUnknownAcceptanceCriterion, initiative.Id, v.CriteriaId)
		}
		if _, dup := seen[v.CriteriaId]; dup {
			return fmt.Errorf("%w: criterion %q answered more than once",
				types.ErrInvalidAcceptanceCriteria, v.CriteriaId)
		}
		seen[v.CriteriaId] = struct{}{}
	}
	return nil
}

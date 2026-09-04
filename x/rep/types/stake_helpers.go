package types

// IsContentConvictionType returns true if the target type is a content conviction stake.
func IsContentConvictionType(t StakeTargetType) bool {
	switch t {
	case StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT:
		return true
	default:
		return false
	}
}

// IsAuthorBondType returns true if the target type is an author bond stake.
func IsAuthorBondType(t StakeTargetType) bool {
	switch t {
	case StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
		StakeTargetType_STAKE_TARGET_BLOG_REPLY_AUTHOR_BOND:
		return true
	default:
		return false
	}
}

// IsContentOrBondType returns true if the target type is any content conviction or author bond stake.
func IsContentOrBondType(t StakeTargetType) bool {
	return IsContentConvictionType(t) || IsAuthorBondType(t)
}

// IsSeasonalPoolType returns true if the target type draws its rewards from the
// shared seasonal staking reward pool (SeasonalPoolAccPerShare) and therefore
// counts toward SeasonalPoolTotalStaked.
func IsSeasonalPoolType(t StakeTargetType) bool {
	switch t {
	case StakeTargetType_STAKE_TARGET_INITIATIVE,
		StakeTargetType_STAKE_TARGET_PROJECT:
		return true
	default:
		return false
	}
}

// IsRewardBearingType returns true if the target type participates in
// MasterChef-style reward accounting (an accumulator plus a per-stake
// reward_debt baseline). Content conviction and author bond stakes earn no
// DREAM and are excluded.
func IsRewardBearingType(t StakeTargetType) bool {
	switch t {
	case StakeTargetType_STAKE_TARGET_INITIATIVE,
		StakeTargetType_STAKE_TARGET_PROJECT,
		StakeTargetType_STAKE_TARGET_MEMBER,
		StakeTargetType_STAKE_TARGET_TAG:
		return true
	default:
		return false
	}
}

// ContentTypeToAuthorBondType maps a content conviction target type to its corresponding author bond type.
// Returns the input unchanged if it's not a content conviction type.
func ContentTypeToAuthorBondType(t StakeTargetType) StakeTargetType {
	switch t {
	case StakeTargetType_STAKE_TARGET_BLOG_CONTENT:
		return StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND
	case StakeTargetType_STAKE_TARGET_FORUM_CONTENT:
		return StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND
	case StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT:
		return StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND
	default:
		return t
	}
}

// AuthorBondTypeToContentType maps an author bond target type to its corresponding content conviction type.
// Returns the input unchanged if it's not an author bond type.
func AuthorBondTypeToContentType(t StakeTargetType) StakeTargetType {
	switch t {
	case StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		StakeTargetType_STAKE_TARGET_BLOG_REPLY_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_BLOG_CONTENT
	case StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_FORUM_CONTENT
	case StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT
	default:
		return t
	}
}

// IsInitiativeTerminal reports whether an initiative has reached a status it
// can never leave — the work is done, rejected, or retired.
//
// Kept in one place because four call sites now depend on the set agreeing:
// reference validation, review-bounty settlement, the project sweep, and
// stakeAccruing, which stops seasonal accrual on terminal initiatives. A stake
// still earning against an initiative one of the others considers finished is
// exactly the drift this prevents.
func IsInitiativeTerminal(status InitiativeStatus) bool {
	switch status {
	case InitiativeStatus_INITIATIVE_STATUS_COMPLETED,
		InitiativeStatus_INITIATIVE_STATUS_REJECTED,
		InitiativeStatus_INITIATIVE_STATUS_CLOSED:
		return true
	}
	return false
}

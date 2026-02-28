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
		StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND:
		return true
	default:
		return false
	}
}

// IsContentOrBondType returns true if the target type is any content conviction or author bond stake.
func IsContentOrBondType(t StakeTargetType) bool {
	return IsContentConvictionType(t) || IsAuthorBondType(t)
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
	case StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_BLOG_CONTENT
	case StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_FORUM_CONTENT
	case StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND:
		return StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT
	default:
		return t
	}
}

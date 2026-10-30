package types

import "cosmossdk.io/collections"

// UserReactionLimitKey is the prefix to retrieve all UserReactionLimit
var UserReactionLimitKey = collections.NewPrefix("userReactionLimit/value/")

// UserReactionLimitStoreKey is the UserReactionLimit map key of the kind's
// budget for addr. The shared reaction budget is keyed by the bare address,
// the others by "<kind>_<address>" (a bech32 address holds no '_').
func UserReactionLimitStoreKey(kind ReactionLimitKind, addr string) string {
	switch kind {
	case ReactionLimitKind_REACTION_LIMIT_KIND_FLAG:
		return "flag_" + addr
	case ReactionLimitKind_REACTION_LIMIT_KIND_DOWNVOTE:
		return "downvote_" + addr
	case ReactionLimitKind_REACTION_LIMIT_KIND_FOLLOW:
		return "follow_" + addr
	default:
		return addr
	}
}

// StoreKey is the map key the record is stored under.
func (m UserReactionLimit) StoreKey() string {
	return UserReactionLimitStoreKey(m.Kind, m.UserAddress)
}

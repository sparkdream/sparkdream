package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "blog"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"

	// Unique Id for posts
	PostKey = "Post/value/"

	// Id of the latest post added to the store
	PostCountKey = "Post/count/"

	// Creator post index
	CreatorPostKey = "Post/creator/"

	// Tag post index: (tag, postID) -> nothing. Maintained by Create/Update/Delete.
	TagPostKey = "Post/tag/"

	// Reply storage
	ReplyKey      = "Reply/value/"
	ReplyCountKey = "Reply/count/"
	ReplyPostKey  = "Reply/post/"

	// Reaction storage
	ReactionKey        = "Reaction/value/"
	ReactionCountKey   = "Reaction/counts/"
	ReactionCreatorKey = "Reaction/creator/"

	// Rate limiting
	RateLimitKey = "RateLimit/"

	// Expiry index
	ExpiryKey = "Expiry/"

	// EphemeralByAuthorKey indexes still-ephemeral content by (creator, kind, id)
	// so the EndBlocker promotion drain can locate a queued author's pending
	// content in O(promoted) without scanning the global expiry index. Only
	// non-anonymous (non-module-creator) ephemerals are tracked. Maintained in
	// lockstep with ExpiryKey.
	EphemeralByAuthorKey = "Ephemeral/creator/"

	// PromotionQueueKey holds the set of authors with ephemeral content
	// awaiting eager promotion to permanent (enqueued when the author becomes a
	// member). Key = creator bech32 string, value = enqueue block-height (8B
	// big-endian) for telemetry only. Drained by the EndBlocker.
	PromotionQueueKey = "Promotion/queue/"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_blog")

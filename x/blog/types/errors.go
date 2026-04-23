package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/blog module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	ErrNotMember              = errors.Register(ModuleName, 1200, "address is not an active member")
	ErrPostNotFound           = errors.Register(ModuleName, 1202, "post not found")
	ErrPostDeleted            = errors.Register(ModuleName, 1203, "post has been deleted")
	ErrPostHidden             = errors.Register(ModuleName, 1204, "post is hidden")
	ErrPostNotHidden          = errors.Register(ModuleName, 1205, "post is not hidden")
	ErrReplyNotFound          = errors.Register(ModuleName, 1206, "reply not found")
	ErrReplyDeleted           = errors.Register(ModuleName, 1207, "reply has been deleted")
	ErrReplyHidden            = errors.Register(ModuleName, 1208, "reply is hidden")
	ErrReplyNotHidden         = errors.Register(ModuleName, 1209, "reply is not hidden")
	ErrRepliesDisabled        = errors.Register(ModuleName, 1210, "replies are disabled for this post")
	ErrMaxReplyDepth          = errors.Register(ModuleName, 1211, "maximum reply depth exceeded")
	ErrUnauthorized           = errors.Register(ModuleName, 1212, "unauthorized")
	ErrRateLimitExceeded      = errors.Register(ModuleName, 1213, "rate limit exceeded")
	ErrInvalidReactionType    = errors.Register(ModuleName, 1214, "invalid reaction type")
	ErrReactionNotFound       = errors.Register(ModuleName, 1215, "reaction not found")
	ErrContentNotEphemeral    = errors.Register(ModuleName, 1216, "content is not ephemeral")
	ErrAlreadyPinned          = errors.Register(ModuleName, 1217, "content is already pinned")
	ErrInsufficientTrustLevel = errors.Register(ModuleName, 1218, "insufficient trust level")
	ErrPostExpired            = errors.Register(ModuleName, 1222, "post has expired")
	ErrReplyExpired           = errors.Register(ModuleName, 1223, "reply has expired")
	ErrInvalidInitiativeRef   = errors.Register(ModuleName, 1224, "invalid initiative reference")

	ErrInvalidTag       = errors.Register(ModuleName, 1225, "invalid tag format")
	ErrMaxTagLength     = errors.Register(ModuleName, 1226, "tag exceeds maximum length")
	ErrTagLimitExceeded = errors.Register(ModuleName, 1227, "tag limit exceeded for post")
	ErrTagNotFound      = errors.Register(ModuleName, 1228, "tag not found")
	ErrReservedTag      = errors.Register(ModuleName, 1229, "tag is reserved")
)

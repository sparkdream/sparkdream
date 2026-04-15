package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/rep module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	// DREAM token errors
	ErrInvalidAmount          = errors.Register(ModuleName, 1101, "invalid amount")
	ErrMemberNotFound         = errors.Register(ModuleName, 1102, "member not found")
	ErrInsufficientBalance    = errors.Register(ModuleName, 1103, "insufficient balance")
	ErrInsufficientStake      = errors.Register(ModuleName, 1104, "insufficient staked DREAM")
	ErrCannotTransferToSelf   = errors.Register(ModuleName, 1105, "cannot transfer to self")
	ErrInvalidTransferPurpose = errors.Register(ModuleName, 1106, "invalid transfer purpose")
	ErrExceedsMaxTipAmount    = errors.Register(ModuleName, 1107, "exceeds maximum tip amount")
	ErrExceedsMaxTipsPerEpoch = errors.Register(ModuleName, 1108, "exceeds maximum tips per epoch")
	ErrRecipientNotActive     = errors.Register(ModuleName, 1109, "recipient is not active")
	ErrExceedsMaxGiftAmount   = errors.Register(ModuleName, 1110, "exceeds maximum gift amount")
	ErrGiftOnlyToInvitees     = errors.Register(ModuleName, 1111, "gifts only allowed to invitees")
	ErrGiftCooldownNotMet     = errors.Register(ModuleName, 1112, "gift cooldown period not met for this recipient")
	ErrExceedsEpochGiftLimit  = errors.Register(ModuleName, 1113, "exceeds maximum gifts per epoch")

	// Invitation errors
	ErrNoInvitationCredits     = errors.Register(ModuleName, 1201, "no invitation credits available")
	ErrMemberAlreadyExists     = errors.Register(ModuleName, 1202, "member already exists")
	ErrInvitationAlreadyExists = errors.Register(ModuleName, 1203, "invitation already exists for this address")
	ErrInvitationNotFound      = errors.Register(ModuleName, 1204, "invitation not found")
	ErrInvitationNotPending    = errors.Register(ModuleName, 1205, "invitation is not pending")
	ErrInviteeAddressMismatch  = errors.Register(ModuleName, 1206, "invitee address does not match invitation")
	ErrNotMember               = errors.Register(ModuleName, 1207, "address is not a member")

	// Project errors
	ErrProjectNotFound           = errors.Register(ModuleName, 1301, "project not found")
	ErrInvalidProjectStatus      = errors.Register(ModuleName, 1302, "invalid project status")
	ErrInsufficientBudget        = errors.Register(ModuleName, 1303, "insufficient budget")
	ErrUnauthorized              = errors.Register(ModuleName, 1304, "unauthorized: insufficient permissions")
	ErrLargeProjectNeedsCouncil  = errors.Register(ModuleName, 1305, "project budget exceeds threshold; requires council proposal approval")

	// Initiative errors
	ErrInitiativeNotFound      = errors.Register(ModuleName, 1401, "initiative not found")
	ErrInvalidInitiativeStatus = errors.Register(ModuleName, 1402, "invalid initiative status")
	ErrInsufficientReputation  = errors.Register(ModuleName, 1403, "insufficient reputation for tier")
	ErrSelfAssignment          = errors.Register(ModuleName, 1404, "cannot self-assign initiative")
	ErrNotAssignee             = errors.Register(ModuleName, 1405, "not the assignee of this initiative")
	ErrTagNotRegistered        = errors.Register(ModuleName, 1406, "tag not registered in forum tag registry")
	ErrTooManyTags             = errors.Register(ModuleName, 1407, "too many tags on initiative")

	// Stake errors
	ErrStakeNotFound     = errors.Register(ModuleName, 1501, "stake not found")
	ErrNotStakeOwner     = errors.Register(ModuleName, 1502, "not the owner of this stake")
	ErrMinStakeDuration  = errors.Register(ModuleName, 1503, "minimum stake duration not met")
	ErrSelfMemberStake   = errors.Register(ModuleName, 1504, "cannot stake on yourself")
	ErrInvalidTargetType = errors.Register(ModuleName, 1505, "invalid stake target type")
	ErrStakePoolNotFound = errors.Register(ModuleName, 1506, "stake pool not found")

	// Content conviction / author bond staking errors
	ErrSelfContentStake     = errors.Register(ModuleName, 1507, "cannot stake conviction on own content")
	ErrContentStakeCap      = errors.Register(ModuleName, 1508, "exceeds max content stake per member for this content")
	ErrAuthorBondCap        = errors.Register(ModuleName, 1509, "exceeds max author bond per content item")
	ErrAuthorBondExists     = errors.Register(ModuleName, 1510, "author bond already exists for this content item")
	ErrAuthorBondNotFound   = errors.Register(ModuleName, 1511, "no author bond found for this content item")
	ErrNotContentTargetType = errors.Register(ModuleName, 1512, "target type is not a content conviction type")
	ErrNotAuthorBondType    = errors.Register(ModuleName, 1513, "target type is not an author bond type")
	ErrAuthorBondViaMsg     = errors.Register(ModuleName, 1514, "author bonds must be created via content module, not MsgStake")
	ErrInitiativeStakeCap         = errors.Register(ModuleName, 1515, "exceeds max initiative stake per member for this target")
	ErrInitiativeRewardCapReached = errors.Register(ModuleName, 1516, "season initiative reward minting cap reached")

	// General errors
	ErrInvalidRequest = errors.Register(ModuleName, 1600, "invalid request")

	// Challenge errors
	ErrChallengeNotFound   = errors.Register(ModuleName, 1701, "challenge not found")
	ErrChallengeNotPending = errors.Register(ModuleName, 1702, "challenge is not pending")
	ErrNotChallengeParty   = errors.Register(ModuleName, 1703, "not a party to this challenge")

	// Member status errors
	ErrMemberAlreadyZeroed = errors.Register(ModuleName, 1801, "member is already zeroed")
	ErrMemberNotActive     = errors.Register(ModuleName, 1802, "member is not active")
	ErrCannotZeroCore      = errors.Register(ModuleName, 1803, "cannot zero a core member without governance vote")

	// Circular staking
	ErrCircularMemberStake = errors.Register(ModuleName, 1805, "circular member staking: target already has an active stake on you")

	// Trust tree errors
	ErrTrustTreeNotBuilt = errors.Register(ModuleName, 1901, "member trust tree has not been built yet")

	// Content challenge errors
	ErrContentChallengeNotFound  = errors.Register(ModuleName, 2001, "content challenge not found")
	ErrContentChallengeExists    = errors.Register(ModuleName, 2002, "active content challenge already exists for this content")
	ErrNoAuthorBond              = errors.Register(ModuleName, 2003, "no author bond found on target content")
	ErrCannotChallengeOwnContent = errors.Register(ModuleName, 2004, "cannot challenge your own content")
	ErrContentChallengeNotActive = errors.Register(ModuleName, 2005, "content challenge is not active")
	ErrNotContentAuthor          = errors.Register(ModuleName, 2006, "not the author of the challenged content")
	ErrBondLockedByChallenge     = errors.Register(ModuleName, 2007, "author bond is locked by an active content challenge")
)

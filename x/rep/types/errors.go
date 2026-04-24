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

	// Permissionless creation errors
	ErrInsufficientTrustLevel      = errors.Register(ModuleName, 1902, "trust level too low for permissionless creation")
	ErrPermissionlessTierExceeded  = errors.Register(ModuleName, 1903, "tier exceeds maximum allowed for permissionless projects")
	ErrInsufficientCreationFee     = errors.Register(ModuleName, 1904, "insufficient DREAM balance for creation fee")

	// Tag registry errors
	ErrTagAlreadyExists = errors.Register(ModuleName, 1910, "tag already exists")
	ErrReservedTagName  = errors.Register(ModuleName, 1911, "tag name is reserved")
	ErrInvalidTagName   = errors.Register(ModuleName, 1912, "invalid tag name")
	ErrTagLimitExceeded = errors.Register(ModuleName, 1913, "tag registry limit exceeded")

	// Tag moderation errors
	ErrTagNotFound            = errors.Register(ModuleName, 1914, "tag not found")
	ErrTagReportNotFound      = errors.Register(ModuleName, 1915, "tag report not found")
	ErrTagReportAlreadyExists = errors.Register(ModuleName, 1916, "report already exists")
	ErrMaxTagReporters        = errors.Register(ModuleName, 1917, "maximum tag reporters reached")
	ErrTagReportNotAuthorized = errors.Register(ModuleName, 1918, "not governance authority")

	// Tag budget errors
	ErrTagBudgetNotFound      = errors.Register(ModuleName, 1919, "tag budget not found")
	ErrTagBudgetNotActive     = errors.Register(ModuleName, 1920, "tag budget is not active")
	ErrTagBudgetInsufficient  = errors.Register(ModuleName, 1921, "insufficient funds in tag budget")
	ErrNotGroupMember         = errors.Register(ModuleName, 1922, "not a member of the budget group")
	ErrNotGroupAccount        = errors.Register(ModuleName, 1923, "not a valid group account")
	ErrTagBudgetAlreadyExists = errors.Register(ModuleName, 1924, "tag budget already exists for this tag")
	ErrPostNotFound           = errors.Register(ModuleName, 1925, "post not found")
	ErrInvalidTag             = errors.Register(ModuleName, 1926, "invalid tag")

	// Sentinel errors
	ErrSentinelNotFound           = errors.Register(ModuleName, 1930, "sentinel not found")
	ErrSentinelDemoted            = errors.Register(ModuleName, 1931, "sentinel is demoted")
	ErrInsufficientSentinelBond   = errors.Register(ModuleName, 1932, "insufficient sentinel bond")
	ErrSentinelCooldownActive     = errors.Register(ModuleName, 1933, "sentinel cooldown active")
	ErrBondAmountTooSmall         = errors.Register(ModuleName, 1934, "bond amount below minimum")
	ErrCannotUnbondPendingActions = errors.Register(ModuleName, 1935, "cannot unbond while sentinel actions are pending")
	ErrDemotionCooldown           = errors.Register(ModuleName, 1936, "sentinel demotion cooldown active")

	// Bonded-role errors (generic; sentinel/curator/verifier share these).
	ErrInvalidRoleType         = errors.Register(ModuleName, 1937, "invalid role type")
	ErrBondedRoleNotFound      = errors.Register(ModuleName, 1938, "bonded role not found")
	ErrBondedRoleConfigMissing = errors.Register(ModuleName, 1953, "bonded role config missing")
	ErrInsufficientBond        = errors.Register(ModuleName, 1954, "insufficient bond")

	// Accountability errors
	ErrReportNotFound          = errors.Register(ModuleName, 1939, "report not found")
	ErrReportAlreadyExists     = errors.Register(ModuleName, 1940, "report already exists")
	ErrCannotReportSelf        = errors.Register(ModuleName, 1941, "cannot report yourself")
	ErrMaxReportersReached     = errors.Register(ModuleName, 1942, "maximum reporters reached")
	ErrAlreadyCosigned         = errors.Register(ModuleName, 1943, "already co-signed this report")
	ErrDefenseAlreadySubmitted = errors.Register(ModuleName, 1944, "defense already submitted")
	ErrDefenseWaitPeriod       = errors.Register(ModuleName, 1945, "must wait after defense before resolution")
	ErrReportNotPending        = errors.Register(ModuleName, 1946, "report is not pending")
	ErrNotGovAuthority         = errors.Register(ModuleName, 1947, "not governance authority")
	ErrAppealAlreadyFiled      = errors.Register(ModuleName, 1948, "appeal already filed for this action")
	ErrInvalidReasonCode       = errors.Register(ModuleName, 1949, "invalid reason code")
	ErrAppealNotFound          = errors.Register(ModuleName, 1950, "gov action appeal not found")
	ErrAppealNotPending        = errors.Register(ModuleName, 1951, "gov action appeal is not pending")
	ErrInvalidAppealVerdict    = errors.Register(ModuleName, 1952, "invalid appeal verdict")

	// Content challenge errors
	ErrContentChallengeNotFound  = errors.Register(ModuleName, 2001, "content challenge not found")
	ErrContentChallengeExists    = errors.Register(ModuleName, 2002, "active content challenge already exists for this content")
	ErrNoAuthorBond              = errors.Register(ModuleName, 2003, "no author bond found on target content")
	ErrCannotChallengeOwnContent = errors.Register(ModuleName, 2004, "cannot challenge your own content")
	ErrContentChallengeNotActive = errors.Register(ModuleName, 2005, "content challenge is not active")
	ErrNotContentAuthor          = errors.Register(ModuleName, 2006, "not the author of the challenged content")
	ErrBondLockedByChallenge     = errors.Register(ModuleName, 2007, "author bond is locked by an active content challenge")

	// Per-member active-work caps
	ErrTooManyActiveInitiatives = errors.Register(ModuleName, 2101, "member has reached the max active initiatives cap")
	ErrTooManyActiveInterims    = errors.Register(ModuleName, 2102, "member has reached the max active interims cap")

	// Global DREAM emission cap
	ErrDreamMintCapExceeded = errors.Register(ModuleName, 2103, "DREAM minting would exceed the per-epoch cap")
)

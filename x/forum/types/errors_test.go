package types

import (
	"testing"

	"cosmossdk.io/errors"
)

// TestErrorCodesUnique verifies that every registered forum error has a
// distinct code. Duplicate codes would break error identification at the
// SDK level.
func TestErrorCodesUnique(t *testing.T) {
	errs := []*errors.Error{
		ErrInvalidSigner,
		ErrUnauthorized,
		ErrContentTooLarge,
		ErrInvalidTag,
		ErrTagLimitExceeded,
		ErrTagNotFound,
		ErrReservedTag,
		ErrEmptyContent,
		ErrInvalidReasonCode,
		ErrReasonTextRequired,
		ErrMaxTagLength,
		ErrPostNotFound,
		ErrNotPostAuthor,
		ErrPostAlreadyHidden,
		ErrPostNotHidden,
		ErrPostDeleted,
		ErrPostArchived,
		ErrNotRootPost,
		ErrCannotDeleteHiddenPost,
		ErrMaxReplyDepthExceeded,
		ErrParentPostNotFound,
		ErrThreadLocked,
		ErrThreadNotLocked,
		ErrThreadAlreadyLocked,
		ErrNotThreadAuthor,
		ErrLockReasonRequired,
		ErrMoveReasonRequired,
		ErrCannotMoveReservedTag,
		ErrCategoryNotFound,
		ErrMembersOnlyWrite,
		ErrAdminOnlyWrite,
		ErrInvalidCategoryId,
		ErrRateLimitExceeded,
		ErrReactionLimitExceeded,
		ErrFlagLimitExceeded,
		ErrFollowLimitExceeded,
		ErrDownvoteLimitExceeded,
		ErrSentinelCooldown,
		ErrInsufficientBond,
		ErrInsufficientReputation,
		ErrSentinelDemoted,
		ErrNotSentinel,
		ErrHideLimitExceeded,
		ErrLockLimitExceeded,
		ErrMoveLimitExceeded,
		ErrInsufficientLockBond,
		ErrInsufficientLockBacking,
		ErrNotGovAuthority,
		ErrNotAuthorized,
		ErrPostStatus,
		ErrAppealCooldown,
		ErrAppealPending,
		ErrGovLockNotAppealable,
		ErrLockAppealAlreadyFiled,
		ErrLockAppealExpired,
		ErrMoveAppealAlreadyFiled,
		ErrMoveAppealExpired,
		ErrThreadNotInactive,
		ErrArchiveCooldown,
		ErrUnarchiveCooldown,
		ErrArchiveCycleLimit,
		ErrArchivedThreadNotFound,
		ErrCannotArchiveThreadWithPendingAppeal,
		ErrBountyNotFound,
		ErrBountyNotActive,
		ErrBountyExpired,
		ErrBountyAlreadyExists,
		ErrNotBountyCreator,
		ErrBountyAmountTooSmall,
		ErrBountyAlreadyAwarded,
		ErrMaxBountyWinners,
		ErrInvalidBountyDuration,
		ErrBountyInModeration,
		ErrNotReplyInThread,
		ErrCannotAwardSelf,
		ErrAlreadyFlagged,
		ErrFlagNotFound,
		ErrNotInReviewQueue,
		ErrMaxFlaggersReached,
		ErrNoAcceptedReply,
		ErrAlreadyAccepted,
		ErrNoProposedReply,
		ErrProposalAlreadyPending,
		ErrMaxPinnedReplies,
		ErrAlreadyPinned,
		ErrNotPinned,
		ErrCannotPinOwnReply,
		ErrPinDisputed,
		ErrCannotDisputeGovPin,
		ErrAlreadyDisputed,
		ErrAlreadyFollowing,
		ErrNotFollowing,
		ErrCannotVoteOwnPost,
		ErrAlreadyVoted,
		ErrForumPaused,
		ErrModerationPaused,
		ErrAppealsPaused,
		ErrBountiesDisabled,
		ErrReactionsDisabled,
		ErrEditingDisabled,
		ErrEditWindowExpired,
		ErrCannotEditHiddenPost,
		ErrCannotEditDeletedPost,
		ErrInvalidAmount,
		ErrInvalidInitiativeRef,
	}

	seen := make(map[uint32]*errors.Error, len(errs))
	for _, e := range errs {
		if e == nil {
			t.Fatal("nil error registered")
		}
		if e.Codespace() != ModuleName {
			t.Errorf("error %v has codespace %q, want %q", e, e.Codespace(), ModuleName)
		}
		if existing, ok := seen[e.ABCICode()]; ok {
			t.Errorf("duplicate code %d: %v vs %v", e.ABCICode(), existing, e)
		}
		seen[e.ABCICode()] = e
	}
}

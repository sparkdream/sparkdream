package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestIssueWarning_AllocatesIDAndPerMemberNumber(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	member := sdk.AccAddress([]byte("phoenix_one")).String()
	issuer := sdk.AccAddress([]byte("forum_module")).String()

	require.NoError(t, k.IssueWarning(ctx, member, issuer, "promoted_hidden_content", []uint64{42}))
	require.NoError(t, k.IssueWarning(ctx, member, issuer, "promoted_hidden_content", []uint64{43}))

	var collected []types.MemberWarning
	require.NoError(t, k.MemberWarning.Walk(ctx, nil, func(_ uint64, w types.MemberWarning) (bool, error) {
		if w.Member == member {
			collected = append(collected, w)
		}
		return false, nil
	}))

	require.Len(t, collected, 2, "two warnings should land in the store")
	// IDs are distinct (allocated from MemberWarningSeq).
	require.NotEqual(t, collected[0].Id, collected[1].Id)

	// warning_number is per-member: first warning is 1, second is 2 — regardless
	// of how many warnings exist for *other* members.
	numbers := map[uint64]bool{}
	for _, w := range collected {
		numbers[w.WarningNumber] = true
		require.Equal(t, issuer, w.IssuedBy)
		require.Equal(t, "promoted_hidden_content", w.Reason)
		require.Len(t, w.EvidencePostIds, 1)
	}
	require.True(t, numbers[1] && numbers[2], "warning_number must enumerate 1 then 2")
}

func TestIssueWarning_RejectsEmptyReason(t *testing.T) {
	f := initFixture(t)
	member := sdk.AccAddress([]byte("phoenix_one")).String()
	issuer := sdk.AccAddress([]byte("forum_module")).String()

	err := f.keeper.IssueWarning(f.ctx, member, issuer, "", []uint64{1})
	require.ErrorIs(t, err, types.ErrEmptyWarningReason)
}

func TestIssueWarning_RejectsInvalidMember(t *testing.T) {
	f := initFixture(t)
	issuer := sdk.AccAddress([]byte("forum_module")).String()

	err := f.keeper.IssueWarning(f.ctx, "not-a-bech32", issuer, "promoted_hidden_content", nil)
	require.Error(t, err, "invalid member address must be rejected")
}

// Per-member numbering: two distinct members can each hold warning_number=1
// in parallel. The sequence is per-member, not global.
func TestIssueWarning_PerMemberNumberingIsIndependent(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	memberA := sdk.AccAddress([]byte("phoenix_two")).String()
	memberB := sdk.AccAddress([]byte("aurora_two")).String()
	issuer := sdk.AccAddress([]byte("forum_module")).String()

	require.NoError(t, k.IssueWarning(f.ctx, memberA, issuer, "reason_a", nil))
	require.NoError(t, k.IssueWarning(f.ctx, memberB, issuer, "reason_b", nil))

	var aWarning, bWarning *types.MemberWarning
	require.NoError(t, k.MemberWarning.Walk(f.ctx, nil, func(_ uint64, w types.MemberWarning) (bool, error) {
		warning := w
		switch warning.Member {
		case memberA:
			aWarning = &warning
		case memberB:
			bWarning = &warning
		}
		return false, nil
	}))

	require.NotNil(t, aWarning)
	require.NotNil(t, bWarning)
	require.Equal(t, uint64(1), aWarning.WarningNumber, "memberA's first warning is #1")
	require.Equal(t, uint64(1), bWarning.WarningNumber, "memberB's first warning is also #1")
}

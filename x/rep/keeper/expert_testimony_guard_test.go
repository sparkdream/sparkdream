package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// An expert witness testifies once, and only while the review is pending.
func TestExpertTestimonyOncePerExpertWhilePending(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	expert := sdk.AccAddress([]byte("expert_phoenix______")[:20])
	require.NoError(t, k.Member.Set(ctx, expert.String(), types.Member{Address: expert.String(), Status: types.MemberStatus_MEMBER_STATUS_ACTIVE}))
	review := types.JuryReview{
		Id: 1, Verdict: types.Verdict_VERDICT_PENDING, ExpertWitnesses: []string{expert.String()},
		ExpertInviters: []string{"juror_a"}, ExpertEndorsers: []string{"juror_b"},
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))

	require.NoError(t, k.SubmitExpertTestimony(ctx, review.Id, expert, sampleOpinion, "matches the criteria"))
	require.Error(t, k.SubmitExpertTestimony(ctx, review.Id, expert, sampleOpinion, "repeat"), "a second testimony is refused")

	review, err := k.JuryReview.Get(ctx, review.Id)
	require.NoError(t, err)
	require.Len(t, review.Testimonies, 1)
	review.Testimonies = nil
	review.Verdict = types.Verdict_VERDICT_UPHOLD_CHALLENGE
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.Error(t, k.SubmitExpertTestimony(ctx, review.Id, expert, sampleOpinion, "after the verdict"), "testimony after the verdict is refused")
}

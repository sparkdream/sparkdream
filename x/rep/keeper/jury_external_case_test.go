package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// x/service jury cases (service.slash, service.controller_transfer) used to be
// opened with CreateAppealInitiative alone: no jurors, not in the PENDING
// index, so they could never reach a verdict and every contested slash was
// voided by the AUTO_TIMEOUT sweep. CreateSeatedAppeal seats the jury,
// excludes the parties, and lets the tally run without touching any
// challenge.
func TestCreateSeatedAppeal(t *testing.T) {
	t.Run("seats a jury without the parties and tallies to a verdict", func(t *testing.T) {
		f := initFixture(t)
		var jurors []string
		for i := 0; i < 6; i++ {
			jurors = append(jurors, mkJurorMember(t, f, fmt.Sprintf("svc_juror_%02d", i), "100.0").String())
		}
		operator := jurors[0] // a party that is also an eligible member
		height := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

		id, err := f.keeper.CreateSeatedAppeal(f.ctx, "service.slash", []byte(`{}`), height+100, []string{operator})
		require.NoError(t, err)
		review, err := f.keeper.GetJuryReview(f.ctx, id)
		require.NoError(t, err)
		require.NotEmpty(t, review.Jurors)
		require.NotContains(t, review.Jurors, operator)
		has, _ := f.keeper.JuryReviewsByVerdict.Has(f.ctx, collections.Join(int32(types.Verdict_VERDICT_PENDING), id))
		require.True(t, has, "the case must be in the PENDING index so the deadline sweep tallies it")

		// Seated jurors can vote; the deciding vote tallies without erroring
		// on the (non-existent) challenge 0.
		for i := 0; i < int(review.RequiredVotes); i++ {
			addr, _ := sdk.AccAddressFromBech32(review.Jurors[i])
			require.NoError(t, f.keeper.SubmitJurorVote(f.ctx, id, addr, nil, types.Verdict_VERDICT_UPHOLD_CHALLENGE,
				math.LegacyMustNewDecFromStr("0.9"), "upheld"))
		}
		review, _ = f.keeper.GetJuryReview(f.ctx, id)
		require.Equal(t, types.Verdict_VERDICT_UPHOLD_CHALLENGE, review.Verdict)
		has, _ = f.keeper.JuryReviewsByVerdict.Has(f.ctx, collections.Join(int32(types.Verdict_VERDICT_PENDING), id))
		require.False(t, has)
	})

	t.Run("an unvoted case resolves INCONCLUSIVE at its deadline", func(t *testing.T) {
		f := initFixture(t)
		for i := 0; i < 6; i++ {
			mkJurorMember(t, f, fmt.Sprintf("svc_juror_%02d", i), "100.0")
		}
		ctx := sdk.UnwrapSDKContext(f.ctx)
		id, err := f.keeper.CreateSeatedAppeal(f.ctx, "service.controller_transfer", []byte(`{}`), ctx.BlockHeight()+10, nil)
		require.NoError(t, err)

		later := ctx.WithBlockHeight(ctx.BlockHeight() + 10)
		require.NoError(t, f.keeper.ResolveExpiredChallengeJuryReviews(later))
		review, _ := f.keeper.GetJuryReview(later, id)
		require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, review.Verdict)
	})

	t.Run("refuses non-external case types", func(t *testing.T) {
		f := initFixture(t)
		_, err := f.keeper.CreateSeatedAppeal(f.ctx, "moderation_appeal", nil, 1, nil)
		require.Error(t, err)
	})
}

package keeper_test

import (
	"context"
	"testing"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/stretchr/testify/require"
)

func TestValidateContentRef(t *testing.T) {
	f := initFixture(t)
	rep := newMockRepKeeper()
	juror := sdk.AccAddress([]byte("juror___________")).String()
	rep.Members[juror] = true
	rep.Initiatives = map[uint64]reptypes.Initiative{
		5: {Id: 5, Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, Assignee: juror, CompletedAt: 1_000},
		6: {Id: 6, Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED},
	}
	f.keeper.SetRepKeeper(rep)
	// rep/jury refs need a jury duty in the current season (SEA-30).
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, juror, types.MemberProfile{Address: juror, JuryDutiesCompleted: 1, LastJuryDutySeason: 1}))

	tests := []struct {
		name       string
		contentRef string
		wantErr    error
	}{
		{
			name:       "too few parts",
			contentRef: "bad",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "two parts only",
			contentRef: "blog/post",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "unsupported module",
			contentRef: "unknown/type/1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "blog/post valid",
			contentRef: "blog/post/42",
			wantErr:    nil,
		},
		{
			name:       "blog invalid type",
			contentRef: "blog/comment/1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "blog invalid id",
			contentRef: "blog/post/abc",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "forum/post valid",
			contentRef: "forum/post/7",
			wantErr:    nil,
		},
		{
			name:       "forum invalid type",
			contentRef: "forum/thread/1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "forum invalid id",
			contentRef: "forum/post/xyz",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "collect/collection valid",
			contentRef: "collect/collection/3",
			wantErr:    nil,
		},
		{
			name:       "collect invalid type",
			contentRef: "collect/item/1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "collect invalid id",
			contentRef: "collect/collection/bad",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/initiative valid",
			contentRef: "rep/initiative/5",
			wantErr:    nil,
		},
		{
			name:       "rep/initiative invalid id",
			contentRef: "rep/initiative/abc",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/initiative not completed",
			contentRef: "rep/initiative/6",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/initiative unknown",
			contentRef: "rep/initiative/7",
			wantErr:    types.ErrContentNotFound,
		},
		{
			name:       "rep/initiative leading zero",
			contentRef: "rep/initiative/05",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/jury valid",
			contentRef: "rep/jury/" + juror,
			wantErr:    nil,
		},
		{
			name:       "rep/jury malformed address",
			contentRef: "rep/jury/cosmos1abc",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/jury non-member",
			contentRef: "rep/jury/" + sdk.AccAddress([]byte("stranger________")).String(),
			wantErr:    types.ErrContentNotFound,
		},
		{
			name:       "blog/post leading zero rejected (SEA-08)",
			contentRef: "blog/post/01",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "forum/post plus sign rejected",
			contentRef: "forum/post/+1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep/jury empty address",
			contentRef: "rep/jury/",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "rep unsupported type",
			contentRef: "rep/stake/1",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "artifact/certificate valid",
			contentRef: "artifact/certificate/3/7",
			wantErr:    nil,
		},
		{
			name:       "artifact/certificate missing edition",
			contentRef: "artifact/certificate/3",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "artifact/certificate invalid id",
			contentRef: "artifact/certificate/3/x",
			wantErr:    types.ErrInvalidContentRef,
		},
		{
			name:       "artifact unsupported type",
			contentRef: "artifact/token/3",
			wantErr:    types.ErrInvalidContentRef,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := f.keeper.ValidateContentRef(f.ctx, tc.contentRef)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type stubArtifact map[[2]uint64]bool

func (s stubArtifact) ImpactCertificateInfo(_ context.Context, classID, editionID uint64) (string, int64, bool) {
	if !s[[2]uint64{classID, editionID}] {
		return "", 0, false
	}
	return "", 0, true
}

// TestValidateContentRefCertificateExists checks certificate refs against
// x/artifact once it is wired.
func TestValidateContentRefCertificateExists(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetArtifactKeeper(stubArtifact{{3, 7}: true})
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/3/7"))
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/3/8"), types.ErrContentNotFound)
}

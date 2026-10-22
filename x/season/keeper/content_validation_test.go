package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/season/types"

	"github.com/stretchr/testify/require"
)

func TestValidateContentRef(t *testing.T) {
	f := initFixture(t)

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
			name:       "rep/jury valid",
			contentRef: "rep/jury/cosmos1abc",
			wantErr:    nil,
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

func (s stubArtifact) IsImpactCertificate(_ context.Context, classID, editionID uint64) bool {
	return s[[2]uint64{classID, editionID}]
}

// TestValidateContentRefCertificateExists checks certificate refs against
// x/artifact once it is wired.
func TestValidateContentRefCertificateExists(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetArtifactKeeper(stubArtifact{{3, 7}: true})
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/3/7"))
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/3/8"), types.ErrContentNotFound)
}

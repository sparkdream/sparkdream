package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestIsContentConvictionType(t *testing.T) {
	tests := []struct {
		name     string
		input    types.StakeTargetType
		expected bool
	}{
		{
			name:     "blog content is content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
			expected: true,
		},
		{
			name:     "forum content is content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
			expected: true,
		},
		{
			name:     "collection content is content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
			expected: true,
		},
		{
			name:     "initiative is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			expected: false,
		},
		{
			name:     "project is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_PROJECT,
			expected: false,
		},
		{
			name:     "member is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_MEMBER,
			expected: false,
		},
		{
			name:     "tag is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_TAG,
			expected: false,
		},
		{
			name:     "blog author bond is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
			expected: false,
		},
		{
			name:     "forum author bond is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
			expected: false,
		},
		{
			name:     "collection author bond is not content conviction type",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.IsContentConvictionType(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestIsAuthorBondType(t *testing.T) {
	tests := []struct {
		name     string
		input    types.StakeTargetType
		expected bool
	}{
		{
			name:     "blog author bond is author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
			expected: true,
		},
		{
			name:     "forum author bond is author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
			expected: true,
		},
		{
			name:     "collection author bond is author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
			expected: true,
		},
		{
			name:     "initiative is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			expected: false,
		},
		{
			name:     "project is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_PROJECT,
			expected: false,
		},
		{
			name:     "member is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_MEMBER,
			expected: false,
		},
		{
			name:     "tag is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_TAG,
			expected: false,
		},
		{
			name:     "blog content is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
			expected: false,
		},
		{
			name:     "forum content is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
			expected: false,
		},
		{
			name:     "collection content is not author bond type",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.IsAuthorBondType(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestIsContentOrBondType(t *testing.T) {
	tests := []struct {
		name     string
		input    types.StakeTargetType
		expected bool
	}{
		// Content types -> true
		{"blog content", types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, true},
		{"forum content", types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT, true},
		{"collection content", types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT, true},
		// Bond types -> true
		{"blog author bond", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, true},
		{"forum author bond", types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, true},
		{"collection author bond", types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, true},
		// Others -> false
		{"initiative", types.StakeTargetType_STAKE_TARGET_INITIATIVE, false},
		{"project", types.StakeTargetType_STAKE_TARGET_PROJECT, false},
		{"member", types.StakeTargetType_STAKE_TARGET_MEMBER, false},
		{"tag", types.StakeTargetType_STAKE_TARGET_TAG, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.IsContentOrBondType(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestContentTypeToAuthorBondType(t *testing.T) {
	tests := []struct {
		name     string
		input    types.StakeTargetType
		expected types.StakeTargetType
	}{
		{
			name:     "blog content maps to blog author bond",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
			expected: types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		},
		{
			name:     "forum content maps to forum author bond",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
			expected: types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		},
		{
			name:     "collection content maps to collection author bond",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
			expected: types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
		},
		{
			name:     "initiative returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			expected: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		},
		{
			name:     "project returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_PROJECT,
			expected: types.StakeTargetType_STAKE_TARGET_PROJECT,
		},
		{
			name:     "member returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_MEMBER,
			expected: types.StakeTargetType_STAKE_TARGET_MEMBER,
		},
		{
			name:     "tag returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_TAG,
			expected: types.StakeTargetType_STAKE_TARGET_TAG,
		},
		{
			name:     "blog author bond returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
			expected: types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.ContentTypeToAuthorBondType(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestAuthorBondTypeToContentType(t *testing.T) {
	tests := []struct {
		name     string
		input    types.StakeTargetType
		expected types.StakeTargetType
	}{
		{
			name:     "blog author bond maps to blog content",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
			expected: types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		},
		{
			name:     "forum author bond maps to forum content",
			input:    types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
			expected: types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		},
		{
			name:     "collection author bond maps to collection content",
			input:    types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
			expected: types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
		},
		{
			name:     "initiative returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			expected: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		},
		{
			name:     "blog content returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
			expected: types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		},
		{
			name:     "member returns unchanged",
			input:    types.StakeTargetType_STAKE_TARGET_MEMBER,
			expected: types.StakeTargetType_STAKE_TARGET_MEMBER,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.AuthorBondTypeToContentType(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestContentTypeToAuthorBondType_Roundtrip(t *testing.T) {
	contentTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
	}

	for _, ct := range contentTypes {
		t.Run(ct.String(), func(t *testing.T) {
			bondType := types.ContentTypeToAuthorBondType(ct)
			backToContent := types.AuthorBondTypeToContentType(bondType)
			require.Equal(t, ct, backToContent, "roundtrip should return original type")
		})
	}
}

func TestAuthorBondTypeToContentType_Roundtrip(t *testing.T) {
	bondTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
	}

	for _, bt := range bondTypes {
		t.Run(bt.String(), func(t *testing.T) {
			contentType := types.AuthorBondTypeToContentType(bt)
			backToBond := types.ContentTypeToAuthorBondType(contentType)
			require.Equal(t, bt, backToBond, "roundtrip should return original type")
		})
	}
}

package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffCollectionTags(t *testing.T) {
	tests := []struct {
		name        string
		oldTags     []string
		newTags     []string
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:        "no change",
			oldTags:     []string{"a", "b"},
			newTags:     []string{"a", "b"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "all added (empty old)",
			oldTags:     nil,
			newTags:     []string{"a", "b"},
			wantAdded:   []string{"a", "b"},
			wantRemoved: nil,
		},
		{
			name:        "all removed (empty new)",
			oldTags:     []string{"a", "b"},
			newTags:     nil,
			wantAdded:   nil,
			wantRemoved: []string{"a", "b"},
		},
		{
			name:        "mixed: drop one, add one, keep one",
			oldTags:     []string{"a", "b"},
			newTags:     []string{"a", "c"},
			wantAdded:   []string{"c"},
			wantRemoved: []string{"b"},
		},
		{
			name:        "reorder only is not a diff",
			oldTags:     []string{"a", "b", "c"},
			newTags:     []string{"c", "a", "b"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "preserves newTags order for added",
			oldTags:     []string{"z"},
			newTags:     []string{"c", "b", "a"},
			wantAdded:   []string{"c", "b", "a"},
			wantRemoved: []string{"z"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := diffCollectionTags(tc.oldTags, tc.newTags)
			require.Equal(t, tc.wantAdded, added)
			require.Equal(t, tc.wantRemoved, removed)
		})
	}
}

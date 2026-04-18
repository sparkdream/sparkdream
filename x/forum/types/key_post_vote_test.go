package types

import "testing"

func TestPostVoteKey(t *testing.T) {
	if len(PostVoteKey) == 0 {
		t.Fatal("PostVoteKey is empty")
	}
	if string(PostVoteKey) != "postVote/" {
		t.Errorf("unexpected prefix: %s", string(PostVoteKey))
	}
}

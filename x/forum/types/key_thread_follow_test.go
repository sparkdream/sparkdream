package types

import "testing"

func TestThreadFollowKey(t *testing.T) {
	if len(ThreadFollowKey) == 0 {
		t.Fatal("ThreadFollowKey is empty")
	}
	if string(ThreadFollowKey) != "threadFollow/value/" {
		t.Errorf("unexpected prefix: %s", string(ThreadFollowKey))
	}
}

package types

import "testing"

func TestThreadFollowCountKey(t *testing.T) {
	if len(ThreadFollowCountKey) == 0 {
		t.Fatal("ThreadFollowCountKey is empty")
	}
	if string(ThreadFollowCountKey) != "threadFollowCount/value/" {
		t.Errorf("unexpected prefix: %s", string(ThreadFollowCountKey))
	}
}

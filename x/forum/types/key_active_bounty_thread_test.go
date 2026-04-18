package types

import "testing"

func TestActiveBountyByThreadKey(t *testing.T) {
	if len(ActiveBountyByThreadKey) == 0 {
		t.Fatal("ActiveBountyByThreadKey is empty")
	}
	if string(ActiveBountyByThreadKey) != "activeBountyByThread/" {
		t.Errorf("unexpected prefix: %s", string(ActiveBountyByThreadKey))
	}
}

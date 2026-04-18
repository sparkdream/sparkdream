package types

import "testing"

func TestSentinelActivityKey(t *testing.T) {
	if len(SentinelActivityKey) == 0 {
		t.Fatal("SentinelActivityKey is empty")
	}
	if string(SentinelActivityKey) != "sentinelActivity/value/" {
		t.Errorf("unexpected prefix: %s", string(SentinelActivityKey))
	}
}

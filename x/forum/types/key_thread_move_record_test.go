package types

import "testing"

func TestThreadMoveRecordKey(t *testing.T) {
	if len(ThreadMoveRecordKey) == 0 {
		t.Fatal("ThreadMoveRecordKey is empty")
	}
	if string(ThreadMoveRecordKey) != "threadMoveRecord/value/" {
		t.Errorf("unexpected prefix: %s", string(ThreadMoveRecordKey))
	}
}

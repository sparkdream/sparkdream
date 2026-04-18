package types

import "testing"

func TestThreadLockRecordKey(t *testing.T) {
	if len(ThreadLockRecordKey) == 0 {
		t.Fatal("ThreadLockRecordKey is empty")
	}
	if string(ThreadLockRecordKey) != "threadLockRecord/value/" {
		t.Errorf("unexpected prefix: %s", string(ThreadLockRecordKey))
	}
}

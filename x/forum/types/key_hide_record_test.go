package types

import "testing"

func TestHideRecordKey(t *testing.T) {
	if len(HideRecordKey) == 0 {
		t.Fatal("HideRecordKey is empty")
	}
	if string(HideRecordKey) != "hideRecord/value/" {
		t.Errorf("unexpected prefix: %s", string(HideRecordKey))
	}
}

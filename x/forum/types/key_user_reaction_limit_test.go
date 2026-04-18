package types

import "testing"

func TestUserReactionLimitKey(t *testing.T) {
	if len(UserReactionLimitKey) == 0 {
		t.Fatal("UserReactionLimitKey is empty")
	}
	if string(UserReactionLimitKey) != "userReactionLimit/value/" {
		t.Errorf("unexpected prefix: %s", string(UserReactionLimitKey))
	}
}

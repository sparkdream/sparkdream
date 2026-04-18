package types

import "testing"

func TestUserRateLimitKey(t *testing.T) {
	if len(UserRateLimitKey) == 0 {
		t.Fatal("UserRateLimitKey is empty")
	}
	if string(UserRateLimitKey) != "userRateLimit/value/" {
		t.Errorf("unexpected prefix: %s", string(UserRateLimitKey))
	}
}

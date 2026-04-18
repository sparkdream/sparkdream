package forum_test

import (
	"testing"

	"cosmossdk.io/depinject"

	forum "sparkdream/x/forum/module"
)

func TestIsOnePerModuleType(t *testing.T) {
	var _ depinject.OnePerModuleType = forum.AppModule{}
	forum.AppModule{}.IsOnePerModuleType()
}

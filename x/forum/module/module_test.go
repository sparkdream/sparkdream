package forum_test

import (
	"testing"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/types/module"

	forum "sparkdream/x/forum/module"
	"sparkdream/x/forum/types"
)

func TestAppModuleName(t *testing.T) {
	am := forum.AppModule{}
	if am.Name() != types.ModuleName {
		t.Errorf("Name() = %q, want %q", am.Name(), types.ModuleName)
	}
}

func TestAppModuleConsensusVersion(t *testing.T) {
	am := forum.AppModule{}
	if am.ConsensusVersion() != 1 {
		t.Errorf("ConsensusVersion() = %d, want 1", am.ConsensusVersion())
	}
}

func TestAppModuleInterfaces(t *testing.T) {
	var am interface{} = forum.AppModule{}

	if _, ok := am.(module.AppModuleBasic); !ok {
		t.Error("AppModule does not implement module.AppModuleBasic")
	}
	if _, ok := am.(module.AppModule); !ok {
		t.Error("AppModule does not implement module.AppModule")
	}
	if _, ok := am.(module.HasGenesis); !ok {
		t.Error("AppModule does not implement module.HasGenesis")
	}
	if _, ok := am.(module.HasInvariants); !ok {
		t.Error("AppModule does not implement module.HasInvariants")
	}
	if _, ok := am.(appmodule.AppModule); !ok {
		t.Error("AppModule does not implement appmodule.AppModule")
	}
	if _, ok := am.(appmodule.HasBeginBlocker); !ok {
		t.Error("AppModule does not implement appmodule.HasBeginBlocker")
	}
	if _, ok := am.(appmodule.HasEndBlocker); !ok {
		t.Error("AppModule does not implement appmodule.HasEndBlocker")
	}
}

func TestIsAppModule(t *testing.T) {
	// IsAppModule is a marker method; just ensure it is callable.
	forum.AppModule{}.IsAppModule()
}

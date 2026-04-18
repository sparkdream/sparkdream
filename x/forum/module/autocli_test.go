package forum_test

import (
	"testing"

	module "sparkdream/x/forum/module"
)

func TestAutoCLIOptions(t *testing.T) {
	am := module.AppModule{}
	opts := am.AutoCLIOptions()
	if opts == nil {
		t.Fatal("AutoCLIOptions returned nil")
	}
	if opts.Query == nil {
		t.Fatal("Query service descriptor is nil")
	}
	if opts.Tx == nil {
		t.Fatal("Tx service descriptor is nil")
	}
	if len(opts.Query.RpcCommandOptions) == 0 {
		t.Error("no query RPC commands configured")
	}
	if len(opts.Tx.RpcCommandOptions) == 0 {
		t.Error("no tx RPC commands configured")
	}
	for _, rpc := range opts.Query.RpcCommandOptions {
		if rpc.RpcMethod == "" {
			t.Error("query RPC has empty RpcMethod")
		}
	}
	for _, rpc := range opts.Tx.RpcCommandOptions {
		if rpc.RpcMethod == "" {
			t.Error("tx RPC has empty RpcMethod")
		}
	}
}

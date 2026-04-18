package types

import "testing"

// types.go is an empty package declaration file; this test exists solely to
// keep the 1:1 implementation/test file convention and assert the package
// compiles with the expected module name.
func TestTypesPackageName(t *testing.T) {
	if ModuleName == "" {
		t.Fatal("ModuleName should not be empty")
	}
}

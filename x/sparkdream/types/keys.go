package types

const (
	// ModuleName defines the module name
	ModuleName = "sparkdream"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_sparkdream"
)

var (
	ParamsKey = []byte("p_sparkdream")
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}

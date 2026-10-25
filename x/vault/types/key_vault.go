package types

import "cosmossdk.io/collections"

// VaultKey is the prefix to retrieve all Vault
var VaultKey = collections.NewPrefix("vault/value/")

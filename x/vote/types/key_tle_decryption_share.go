package types

import "cosmossdk.io/collections"

// TleDecryptionShareKey is the prefix to retrieve all TleDecryptionShare
var TleDecryptionShareKey = collections.NewPrefix("tleDecryptionShare/value/")

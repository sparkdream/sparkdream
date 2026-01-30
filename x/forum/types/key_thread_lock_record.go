package types

import "cosmossdk.io/collections"

// ThreadLockRecordKey is the prefix to retrieve all ThreadLockRecord
var ThreadLockRecordKey = collections.NewPrefix("threadLockRecord/value/")

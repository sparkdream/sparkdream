package types

import "cosmossdk.io/collections"

// VoterRegistrationKey is the prefix to retrieve all VoterRegistration
var VoterRegistrationKey = collections.NewPrefix("voterRegistration/value/")

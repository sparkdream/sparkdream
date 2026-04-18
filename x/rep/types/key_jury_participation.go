package types

import "cosmossdk.io/collections"

// JuryParticipationKey is the prefix to retrieve all JuryParticipation entries.
var JuryParticipationKey = collections.NewPrefix("juryParticipation/value/")

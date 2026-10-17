package types

import "sparkdream/tools/tle"

// The submission-proof parameters of encrypted-batch execs are defined in
// tools/tle (shared with the web client); see tle.BatchSubmissionScope.
const (
	BatchSubmissionDomain  = tle.BatchSubmissionDomain
	BatchSubmissionTypeURL = tle.BatchSubmissionTypeURL
)

// BatchSubmissionScope is tle.BatchSubmissionScope.
func BatchSubmissionScope(encryptedPayload []byte) uint64 {
	return tle.BatchSubmissionScope(encryptedPayload)
}

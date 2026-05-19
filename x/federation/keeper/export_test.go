package keeper

import (
	"context"

	"sparkdream/x/federation/types"
)

// export_test.go exposes selected unexported keeper methods to the
// _test package without growing the production API surface. This is a
// standard pattern for tests that exercise internal helpers exhaustively.

// FileChallengeReportForTest wraps fileChallengeReport so the tests in
// msg_server_submit_arbiter_hash_test.go can assert on the
// OpenSystemReport arguments without going through the full
// MsgSubmitArbiterHash / MsgEscalateChallenge flow.
func (k Keeper) FileChallengeReportForTest(ctx context.Context, content types.FederatedContent, evidenceURI string) error {
	return k.fileChallengeReport(ctx, content, evidenceURI)
}

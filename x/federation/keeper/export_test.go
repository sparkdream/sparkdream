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

// Rate-limit helpers re-exported for rate_limit_test.go. The
// production handlers call the unexported variants; these wrappers
// let the _test package drive the same logic with synthetic
// block-time / height values without standing up the full
// MsgSubmitFederatedContent / MsgFederateContent flows.
func (k Keeper) CallCheckAndRecordInboundRate(ctx context.Context, peerID string, blockTime, windowSec int64, limit uint64) error {
	return k.checkAndRecordInboundRate(ctx, peerID, blockTime, windowSec, limit)
}

func (k Keeper) CallCheckAndRecordOutboundRate(ctx context.Context, peerID string, blockTime, windowSec int64, limit uint64) error {
	return k.checkAndRecordOutboundRate(ctx, peerID, blockTime, windowSec, limit)
}

func (k Keeper) CallCheckAndRecordInboundPerBlock(ctx context.Context, height int64, limit uint64) error {
	return k.checkAndRecordInboundPerBlock(ctx, height, limit)
}

func (k Keeper) CallCheckAndRecordOutboundPerBlock(ctx context.Context, height int64, limit uint64) error {
	return k.checkAndRecordOutboundPerBlock(ctx, height, limit)
}

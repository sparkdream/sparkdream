package keeper

import (
	"context"

	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

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
	_, err := k.fileChallengeReport(ctx, content, evidenceURI)
	return err
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

// SetPendingChallengeForTest stores a pending identity challenge through the
// indexing helper, the way the IBC receive path does.
func (k Keeper) SetPendingChallengeForTest(ctx context.Context, c types.PendingIdentityChallenge) error {
	return k.setPendingChallenge(ctx, c)
}

// SetSendPacketOverrideForTest replaces the IBC SendPacket call (nil
// restores it). The fixture has no open channel; tests of paths that fail
// on a send error use this for a send that succeeds or fails on demand.
func (k Keeper) SetSendPacketOverrideForTest(fn func(ctx context.Context, peerID string, data []byte) (uint64, error)) {
	k.late.sendPacketOverride = fn
}

// FindPeerByChannelForTest wraps findPeerByChannel for the channel index
// tests (FED-101).
func (k Keeper) FindPeerByChannelForTest(ctx context.Context, channelID string) (string, error) {
	return k.findPeerByChannel(ctx, channelID)
}

// IBCKeeperForTest returns the late-wired IBC keeper so tests can open the
// channels a Spark Dream peer's activation requires (FED-106).
func (k Keeper) IBCKeeperForTest() *ibckeeper.Keeper {
	if k.late == nil || k.late.ibcKeeperFn == nil {
		return nil
	}
	return k.late.ibcKeeperFn()
}

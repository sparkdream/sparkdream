package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// EndBlocker runs at the end of each block. 12 phases per spec §9.
// BeginBlocker funds the bridge-operator reward pool. Funding is best-effort:
// FundOperatorRewardPool swallows its own failures, and any error surfacing
// here is logged rather than returned, because a funding problem must never
// halt the chain.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	if err := k.FundOperatorRewardPool(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/federation").
			Error("BeginBlocker: operator reward pool funding failed", "error", err)
	}
	return nil
}

func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}

	logger := sdkCtx.Logger().With("module", "x/federation")

	maxPrune := params.MaxPrunePerBlock
	var pruned uint64
	var phaseErr error

	// Phase 1: Prune Expired Federated Content
	pruned, phaseErr = k.pruneExpiredContent(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 1 (prune expired content) failed", "error", phaseErr)
	}

	// Phase 2: Prune Expired Reputation Attestations
	pruned, phaseErr = k.pruneExpiredAttestations(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 2 (prune expired attestations) failed", "error", phaseErr)
	}

	// Phase 3: Prune Expired Unverified Identity Links
	pruned, phaseErr = k.pruneExpiredUnverifiedLinks(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 3 (prune expired unverified links) failed", "error", phaseErr)
	}

	// Phase 4: Prune Expired Identity Challenges
	pruned, phaseErr = k.pruneExpiredIdentityChallenges(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 4 (prune expired identity challenges) failed", "error", phaseErr)
	}

	// Phase 5: Expire Unverified Content
	pruned, phaseErr = k.expireUnverifiedContent(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 5 (expire unverified content) failed", "error", phaseErr)
	}

	// Phase 6: Release Verifier Bond Commitments
	pruned, phaseErr = k.releaseVerifierBondCommitments(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 6 (release verifier bond commitments) failed", "error", phaseErr)
	}

	// Phase 7: Expire Arbiter Resolution Windows
	pruned, phaseErr = k.expireArbiterResolutions(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 7 (expire arbiter resolutions) failed", "error", phaseErr)
	}

	// Phase 8: Finalize Auto-Resolutions
	pruned, phaseErr = k.finalizeAutoResolutions(ctx, now, maxPrune, pruned, params)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 8 (finalize auto-resolutions) failed", "error", phaseErr)
	}

	// Phase 9: Process Peer Removal Queue
	_, phaseErr = k.processPeerRemovalQueue(ctx, sdkCtx, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 9 (process peer removal queue) failed", "error", phaseErr)
	}

	// Phase 10 (verifier epoch rewards) is gone: verifier pay -- both the
	// SPARK pool and the DREAM stipend -- is distributed by x/rep's
	// EndBlocker, which owns the RoleActivity record the payout is scored
	// from. Federation reports verifications and verdicts and pays nothing.
	//
	// Bridge OPERATOR pay does live here, because federation owns the data it
	// is scored from (BridgeBinding submission counters) and operators are
	// x/service Operators rather than x/rep bonded roles. Distribution runs
	// before the overflow burn so the pool drains to the people who earned it
	// first and the burn only ever targets residual.
	if err := k.DistributeOperatorRewards(ctx); err != nil {
		logger.Error("EndBlocker: operator reward distribution failed", "error", err)
	}
	if err := k.BurnOperatorRewardPoolOverflow(ctx); err != nil {
		logger.Error("EndBlocker: operator reward pool overflow burn failed", "error", err)
	}

	// Phase 11: Bridge Operator Monitoring
	if err := k.monitorBridgeOperators(ctx, sdkCtx, now, params); err != nil {
		logger.Error("EndBlocker phase 11 (monitor bridge operators) failed", "error", err)
	}

	// Phase 12: Clean Stale Rate Limit Counters
	if err := k.cleanStaleRateLimitCounters(ctx, now, params); err != nil {
		logger.Error("EndBlocker phase 12 (clean stale rate limit counters) failed", "error", err)
	}

	return nil
}

// --- Phase 1 ---

func (k Keeper) pruneExpiredContent(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ContentExpiration.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		content, err := k.Content.Get(ctx, contentID)
		if err == nil {
			_ = k.ContentByPeer.Remove(ctx, collections.Join(content.PeerId, contentID))
			_ = k.ContentByType.Remove(ctx, collections.Join(content.ContentType, contentID))
			if content.CreatorIdentity != "" {
				_ = k.ContentByCreator.Remove(ctx, collections.Join(content.CreatorIdentity, contentID))
			}
			_ = k.Content.Remove(ctx, contentID)
		}
		_ = k.ContentExpiration.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 2 ---

func (k Keeper) pruneExpiredAttestations(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	err := k.AttestationExp.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		_ = k.RepAttestations.Remove(ctx, collections.Join(key.K2(), key.K3()))
		_ = k.AttestationExp.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 3 ---

func (k Keeper) pruneExpiredUnverifiedLinks(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	err := k.UnverifiedLinkExp.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		localAddr := key.K2()
		peerID := key.K3()

		link, err := k.IdentityLinks.Get(ctx, collections.Join(localAddr, peerID))
		if err == nil && link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED {
			_ = k.IdentityLinks.Remove(ctx, collections.Join(localAddr, peerID))
			_ = k.IdentityLinksByRemote.Remove(ctx, collections.Join(peerID, link.RemoteIdentity))
			cnt, _ := k.IdentityLinkCount.Get(ctx, localAddr)
			if cnt > 0 {
				_ = k.IdentityLinkCount.Set(ctx, localAddr, cnt-1)
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityLinkExpired,
				sdk.NewAttribute(types.AttributeKeyLocalAddress, localAddr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		}
		_ = k.UnverifiedLinkExp.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 4 ---

func (k Keeper) pruneExpiredIdentityChallenges(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	var toDelete []collections.Pair[string, string]
	_ = k.PendingIdChallenges.Walk(ctx, nil, func(key collections.Pair[string, string], val types.PendingIdentityChallenge) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if val.ExpiresAt <= now {
			toDelete = append(toDelete, key)
			pruned++
		}
		return false, nil
	})
	for _, key := range toDelete {
		_ = k.PendingIdChallenges.Remove(ctx, key)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityChallengeExpired,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, key.K1()),
			sdk.NewAttribute(types.AttributeKeyPeerID, key.K2())))
	}
	return pruned, nil
}

// --- Phase 5 ---

func (k Keeper) expireUnverifiedContent(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.VerificationWindow.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		content, err := k.Content.Get(ctx, contentID)
		if err == nil && content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
			_ = k.Content.Set(ctx, contentID, content)

			bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
			binding, berr := k.BridgeBindings.Get(ctx, bridgeKey)
			if berr == nil {
				binding.ContentUnverified++
				binding.EpochUnverified++
				_ = k.BridgeBindings.Set(ctx, bridgeKey, binding)
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentVerificationExpired,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId)))
		}
		_ = k.VerificationWindow.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 6 ---

func (k Keeper) releaseVerifierBondCommitments(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ChallengeWindow.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		record, err := k.VerificationRecords.Get(ctx, contentID)
		if err == nil && record.Outcome == types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING {
			record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CONFIRMED
			// Release the verifier's committed bond back to available and
			// bump per-module unchallenged counter.
			if k.late.repKeeper != nil {
				_ = k.late.repKeeper.ReleaseBond(ctx,
					reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
					record.Verifier, record.CommittedAmount)
			}
			activity, _ := k.VerifierActivity.Get(ctx, record.Verifier)
			if activity.Address == "" {
				activity.Address = record.Verifier
			}
			activity.UnchallengedVerifications++
			_ = k.VerifierActivity.Set(ctx, record.Verifier, activity)
			_ = k.VerificationRecords.Set(ctx, contentID, record)
		}
		_ = k.ChallengeWindow.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 7 ---

func (k Keeper) expireArbiterResolutions(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ArbiterResolutionQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeArbiterResolutionExpired,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID))))
		k.cleanupArbiterData(ctx, contentID)
		_ = k.ArbiterResolutionQueue.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 8 ---

// finalizeAutoResolutions drains two queues sharing the prune budget:
// (a) the arbiter escalation queue, which fires the Phase 1 auto-
// verdict when the escalation window expires without an escalation,
// and (b) the jury deadline queue, which stamps TIMEOUT on
// EscalatedChallenges that the Operations Committee never resolved.
func (k Keeper) finalizeAutoResolutions(ctx context.Context, now int64, maxPrune, pruned uint64, params types.Params) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	if err := k.ArbiterEscalationQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		// Apply any stashed PendingVerifierVerdict before cleaning up
		// arbiter data. Escalation cleared it back to UNSPECIFIED;
		// otherwise this is where the auto-verdict (counter bumps +
		// fee disbursement + slash + content status flip) takes effect.
		k.applyAutoVerdict(ctx, contentID, now, params)
		k.cleanupArbiterData(ctx, contentID)
		_ = k.ArbiterEscalationQueue.Remove(ctx, key)
		pruned++
		return false, nil
	}); err != nil {
		return pruned, err
	}

	if pruned >= maxPrune {
		return pruned, nil
	}
	juryRng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))
	err := k.EscalatedChallengeDeadline.Walk(ctx, juryRng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		// applyJuryVerdict removes both the EscalatedChallenge entry
		// and its deadline-queue entry, so no explicit Remove needed
		// here. Skip silently if the entry is gone (race-safe).
		k.applyJuryVerdict(ctx, contentID, types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT, now, params)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 9 ---

func (k Keeper) processPeerRemovalQueue(ctx context.Context, sdkCtx sdk.Context, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	err := k.PeerRemovalQueue.Walk(ctx, nil, func(peerID string, state types.PeerRemovalState) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if !state.ContentDone {
			rng := collections.NewPrefixedPairRange[string, uint64](peerID)
			_ = k.ContentByPeer.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
				if pruned >= maxPrune {
					return true, nil
				}
				contentID := key.K2()
				content, err := k.Content.Get(ctx, contentID)
				if err == nil {
					_ = k.ContentByType.Remove(ctx, collections.Join(content.ContentType, contentID))
					if content.CreatorIdentity != "" {
						_ = k.ContentByCreator.Remove(ctx, collections.Join(content.CreatorIdentity, contentID))
					}
					_ = k.Content.Remove(ctx, contentID)
				}
				_ = k.ContentByPeer.Remove(ctx, key)
				pruned++
				state.LastPrunedContentId = contentID
				return false, nil
			})
			if pruned < maxPrune {
				state.ContentDone = true
			}
		}
		if state.ContentDone && !state.BridgesDone && pruned < maxPrune {
			// Peer removal during the federation→service migration:
			// bond return is owned by x/service (operator must unbond via
			// service.MsgUnbondOperator first). Per the abandoned-peer
			// escape hatch in Phase 5 of the migration plan, a peer-
			// removal gov proposal may bundle MsgReportOperator
			// (T1_SLASH, dissolve=true) for each active bridge so any
			// stranded operators get dissolved atomically. By the time
			// we reach the cleanup walk, all bindings should reference
			// SLASHED/RETIRED operators whose hooks already cleared
			// federation state — we just prune any orphaned bindings.
			rng := collections.NewPrefixedPairRange[string, string](peerID)
			_ = k.BridgesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
				opAddr := key.K2()
				bindingKey := collections.Join(opAddr, peerID)
				_ = k.BridgeBindings.Remove(ctx, bindingKey)
				_ = k.BridgesByPeer.Remove(ctx, key)
				return false, nil
			})
			state.BridgesDone = true
		}
		if state.BridgesDone && !state.PolicyDone {
			_ = k.PeerPolicies.Remove(ctx, peerID)
			state.PolicyDone = true
		}
		if state.ContentDone && state.BridgesDone && state.PolicyDone {
			_ = k.Peers.Remove(ctx, peerID)
			_ = k.PeerRemovalQueue.Remove(ctx, peerID)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePeerCleanupComplete,
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		} else {
			_ = k.PeerRemovalQueue.Set(ctx, peerID, state)
		}
		return false, nil
	})
	return pruned, err
}

// --- Phase 11 ---

func (k Keeper) monitorBridgeOperators(ctx context.Context, sdkCtx sdk.Context, now int64, params types.Params) error {
	// Bound the walk to maxPrunePerBlock to prevent unbounded iteration every block.
	var checked uint64
	maxCheck := params.MaxPrunePerBlock
	return k.BridgeBindings.Walk(ctx, nil, func(_ collections.Pair[string, string], binding types.BridgeBinding) (bool, error) {
		if checked >= maxCheck {
			return true, nil
		}
		checked++
		// Suspended bindings don't get inactivity warnings (the operator
		// is in UNDERFUNDED state on x/service; warning is redundant).
		if binding.Suspended {
			return false, nil
		}
		epochSec := int64(params.RateLimitWindow.Seconds())
		if epochSec > 0 && binding.LastSubmissionAt > 0 {
			epochsSince := (now - binding.LastSubmissionAt) / epochSec
			if uint64(epochsSince) > params.BridgeInactivityThreshold {
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBridgeInactiveWarning,
					sdk.NewAttribute(types.AttributeKeyOperator, binding.Address),
					sdk.NewAttribute(types.AttributeKeyPeerID, binding.PeerId)))
			}
		}
		// Stake-insufficient monitoring removed: x/service emits
		// service.operator_underfunded directly on the transition, and
		// federation's AfterOperatorUnderfunded hook flips the binding's
		// suspended flag. Off-chain monitors should watch
		// service.operator_underfunded for this signal.
		return false, nil
	})
}

// --- Phase 12 ---

func (k Keeper) cleanStaleRateLimitCounters(ctx context.Context, now int64, params types.Params) error {
	// Bound all walks to maxPrunePerBlock to prevent unbounded iteration.
	maxPrune := params.MaxPrunePerBlock
	var pruned uint64

	// Per-block caps: at most one entry per direction per block. Prune
	// everything strictly below the current height — the current block's
	// entry is still live until EndBlocker returns; deleting it here is
	// fine because no further txs can land in this block.
	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	err := k.InboundPerBlock.Walk(ctx, nil, func(height int64, _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if height < currentHeight {
			_ = k.InboundPerBlock.Remove(ctx, height)
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.OutboundPerBlock.Walk(ctx, nil, func(height int64, _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if height < currentHeight {
			_ = k.OutboundPerBlock.Remove(ctx, height)
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	// Per-peer sliding-window counters: keep two windows of history so
	// the current-window calculation can still consult the previous
	// window; anything older is dead state.
	windowSec := int64(params.RateLimitWindow.Seconds())
	if windowSec <= 0 {
		return nil
	}
	cutoff := now - 2*windowSec

	err = k.InboundRateLimits.Walk(ctx, nil, func(key collections.Pair[string, int64], _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if key.K2() < cutoff {
			_ = k.InboundRateLimits.Remove(ctx, key)
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	return k.OutboundRateLimits.Walk(ctx, nil, func(key collections.Pair[string, int64], _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if key.K2() < cutoff {
			_ = k.OutboundRateLimits.Remove(ctx, key)
			pruned++
		}
		return false, nil
	})
}

// --- Helpers ---

func (k Keeper) cleanupArbiterData(ctx context.Context, contentID uint64) {
	rng := collections.NewPrefixedPairRange[uint64, string](contentID)
	_ = k.ArbiterSubmissions.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ types.ArbiterHashSubmission) (bool, error) {
		_ = k.ArbiterSubmissions.Remove(ctx, key)
		return false, nil
	})
	_ = k.ArbiterHashCounts.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ uint32) (bool, error) {
		_ = k.ArbiterHashCounts.Remove(ctx, key)
		return false, nil
	})
}

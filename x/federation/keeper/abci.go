package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// EndBlocker runs at the end of each block.
// 13 phases as specified in the federation spec Section 9.
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

	// Phase 5: Release Unbonded Bridge Stakes
	pruned, phaseErr = k.releaseUnbondedBridgeStakes(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 5 (release unbonded bridge stakes) failed", "error", phaseErr)
	}

	// Phase 6: Expire Unverified Content
	pruned, phaseErr = k.expireUnverifiedContent(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 6 (expire unverified content) failed", "error", phaseErr)
	}

	// Phase 7: Release Verifier Bond Commitments
	pruned, phaseErr = k.releaseVerifierBondCommitments(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 7 (release verifier bond commitments) failed", "error", phaseErr)
	}

	// Phase 8: Expire Arbiter Resolution Windows
	pruned, phaseErr = k.expireArbiterResolutions(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 8 (expire arbiter resolutions) failed", "error", phaseErr)
	}

	// Phase 9: Finalize Auto-Resolutions
	pruned, phaseErr = k.finalizeAutoResolutions(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 9 (finalize auto-resolutions) failed", "error", phaseErr)
	}

	// Phase 10: Process Peer Removal Queue
	_, phaseErr = k.processPeerRemovalQueue(ctx, sdkCtx, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 10 (process peer removal queue) failed", "error", phaseErr)
	}

	// Phase 11: Verifier Epoch Rewards (TODO: epoch detection + reward distribution)

	// Phase 12: Bridge Operator Monitoring
	if err := k.monitorBridgeOperators(ctx, sdkCtx, now, params); err != nil {
		logger.Error("EndBlocker phase 12 (monitor bridge operators) failed", "error", err)
	}

	// Phase 13: Clean Stale Rate Limit Counters
	if err := k.cleanStaleRateLimitCounters(ctx, now, params); err != nil {
		logger.Error("EndBlocker phase 13 (clean stale rate limit counters) failed", "error", err)
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

func (k Keeper) releaseUnbondedBridgeStakes(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	err := k.BridgeUnbondingQueue.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		operatorAddr := key.K2()
		peerID := key.K3()
		bridgeKey := collections.Join(operatorAddr, peerID)

		bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
		if err == nil && bridge.Status == types.BridgeStatus_BRIDGE_STATUS_UNBONDING {
			if bridge.Stake.Amount.IsPositive() {
				opBytes, _ := k.addressCodec.StringToBytes(operatorAddr)
				_ = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, opBytes, sdk.NewCoins(bridge.Stake))
			}
			bridge.Status = types.BridgeStatus_BRIDGE_STATUS_REVOKED
			_ = k.BridgeOperators.Set(ctx, bridgeKey, bridge)

			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBridgeUnbondingComplete,
				sdk.NewAttribute(types.AttributeKeyOperator, operatorAddr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		}
		_ = k.BridgeUnbondingQueue.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 6 ---

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
			bridge, berr := k.BridgeOperators.Get(ctx, bridgeKey)
			if berr == nil {
				bridge.ContentUnverified++
				_ = k.BridgeOperators.Set(ctx, bridgeKey, bridge)
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

// --- Phase 7 ---

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

// --- Phase 8 ---

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

// --- Phase 9 ---

func (k Keeper) finalizeAutoResolutions(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ArbiterEscalationQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		k.cleanupArbiterData(ctx, key.K2())
		_ = k.ArbiterEscalationQueue.Remove(ctx, key)
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 10 ---

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
			rng := collections.NewPrefixedPairRange[string, string](peerID)
			_ = k.BridgesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
				opAddr := key.K2()
				bridgeKey := collections.Join(opAddr, peerID)
				bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
				if err == nil && bridge.Stake.Amount.IsPositive() {
					opBytes, _ := k.addressCodec.StringToBytes(opAddr)
					_ = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, opBytes, sdk.NewCoins(bridge.Stake))
				}
				_ = k.BridgeOperators.Remove(ctx, bridgeKey)
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

// --- Phase 12 ---

func (k Keeper) monitorBridgeOperators(ctx context.Context, sdkCtx sdk.Context, now int64, params types.Params) error {
	// Bound the walk to maxPrunePerBlock to prevent unbounded iteration every block.
	var checked uint64
	maxCheck := params.MaxPrunePerBlock
	return k.BridgeOperators.Walk(ctx, nil, func(_ collections.Pair[string, string], bridge types.BridgeOperator) (bool, error) {
		if checked >= maxCheck {
			return true, nil
		}
		checked++
		if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
			return false, nil
		}
		epochSec := int64(params.RateLimitWindow.Seconds())
		if epochSec > 0 && bridge.LastSubmissionAt > 0 {
			epochsSince := (now - bridge.LastSubmissionAt) / epochSec
			if uint64(epochsSince) > params.BridgeInactivityThreshold {
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBridgeInactiveWarning,
					sdk.NewAttribute(types.AttributeKeyOperator, bridge.Address),
					sdk.NewAttribute(types.AttributeKeyPeerID, bridge.PeerId)))
			}
		}
		if bridge.Stake.Amount.LT(params.MinBridgeStake.Amount) {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBridgeStakeInsufficient,
				sdk.NewAttribute(types.AttributeKeyOperator, bridge.Address),
				sdk.NewAttribute(types.AttributeKeyPeerID, bridge.PeerId)))
		}
		return false, nil
	})
}

// --- Phase 13 ---

func (k Keeper) cleanStaleRateLimitCounters(ctx context.Context, now int64, params types.Params) error {
	windowSec := int64(params.RateLimitWindow.Seconds())
	if windowSec <= 0 {
		return nil
	}
	cutoff := now - 2*windowSec

	// Bound both walks to maxPrunePerBlock to prevent unbounded iteration.
	maxPrune := params.MaxPrunePerBlock
	var pruned uint64

	err := k.InboundRateLimits.Walk(ctx, nil, func(key collections.Pair[string, int64], _ uint64) (bool, error) {
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

package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	corestore "cosmossdk.io/core/store"

	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	zkcrypto "sparkdream/tools/crypto"
)

// Store keys for the member trust tree.
var (
	memberTrustTreeRootKey         = []byte("trust_tree/root")
	previousMemberTrustTreeRootKey = []byte("trust_tree/prev_root")
	trustTreeDirtyKey              = []byte("trust_tree/dirty") // full-rebuild flag (genesis/upgrade)

	// Incremental persistent tree keys.
	trustTreeNodePrefix        = []byte("trust_tree/node/")
	trustTreeMemberIdxPrefix   = []byte("trust_tree/member_idx/")
	trustTreeIdxMemberPrefix   = []byte("trust_tree/idx_member/")
	trustTreeNextLeafIdxKey    = []byte("trust_tree/next_leaf_idx")
	trustTreeDirtyMemberPrefix = []byte("trust_tree/dirty_member/")
	trustTreeInitializedKey    = []byte("trust_tree/initialized")
)

// trustTreeDepth is the tree depth. Production uses zkcrypto.TreeDepth (20).
// Package-level var so tests can override with a smaller depth.
var trustTreeDepth = zkcrypto.TreeDepth

// zeroHashes[i] is the hash of a fully empty subtree at level i.
// zeroHashes[0] = 32 zero bytes, zeroHashes[i] = Hash(zeroHashes[i-1], zeroHashes[i-1]).
var zeroHashes [][]byte

func init() {
	initZeroHashes(zkcrypto.TreeDepth)
}

func initZeroHashes(depth int) {
	zeroHashes = make([][]byte, depth+1)
	zeroHashes[0] = make([]byte, 32)
	for i := 1; i <= depth; i++ {
		zeroHashes[i] = zkcrypto.HashTwoFields(zeroHashes[i-1], zeroHashes[i-1])
	}
}

// ---------------------------------------------------------------------------
// Public API (unchanged signatures)
// ---------------------------------------------------------------------------

// GetMemberTrustTreeRoot returns the current member trust tree Merkle root.
// Returns nil, error if the tree has not been built yet.
func (k Keeper) GetMemberTrustTreeRoot(ctx context.Context) ([]byte, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(memberTrustTreeRootKey)
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, types.ErrTrustTreeNotBuilt
	}
	return bz, nil
}

// GetPreviousMemberTrustTreeRoot returns the previous member trust tree Merkle root.
// Returns nil if no previous root exists (e.g., first build).
func (k Keeper) GetPreviousMemberTrustTreeRoot(ctx context.Context) []byte {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(previousMemberTrustTreeRootKey)
	if err != nil || len(bz) == 0 {
		return nil
	}
	return bz
}

// MarkTrustTreeDirty sets the full-rebuild flag. Used after genesis import or
// chain upgrades that invalidate the entire tree.
func (k Keeper) MarkTrustTreeDirty(ctx context.Context) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(trustTreeDirtyKey, []byte{1}) //nolint:errcheck
}

// IsTrustTreeDirty returns true if there are any pending tree updates
// (either per-member dirty entries or the full-rebuild flag).
func (k Keeper) IsTrustTreeDirty(ctx context.Context) bool {
	store := k.storeService.OpenKVStore(ctx)

	// Check full-rebuild flag first.
	bz, err := store.Get(trustTreeDirtyKey)
	if err == nil && len(bz) > 0 && bz[0] == 1 {
		return true
	}

	// Check if any per-member dirty entries exist.
	end := prefixEnd(trustTreeDirtyMemberPrefix)
	iter, err := store.Iterator(trustTreeDirtyMemberPrefix, end)
	if err != nil {
		return false
	}
	defer iter.Close()
	return iter.Valid()
}

// MarkMemberDirty adds a single member address to the dirty set.
// On the next EndBlock, only this member's leaf will be recomputed.
func (k Keeper) MarkMemberDirty(ctx context.Context, address string) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(dirtyMemberKey(address), []byte{1}) //nolint:errcheck
}

// MaybeRebuildTrustTree is called from EndBlocker. It handles:
//  1. First-time initialization (populates tree from all active members with ZK keys).
//  2. Full rebuild (after genesis import or upgrade).
//  3. Incremental updates (processes only dirty members).
func (k Keeper) MaybeRebuildTrustTree(ctx context.Context) error {
	// 1. First-time initialization.
	if !k.isTrustTreeInitialized(ctx) {
		return k.initializeTrustTree(ctx)
	}

	// 2. Full rebuild requested (genesis/upgrade).
	if k.isFullRebuildRequested(ctx) {
		return k.fullRebuildTrustTree(ctx)
	}

	// 3. Incremental updates.
	dirtyAddrs := k.getDirtyMembers(ctx)
	if len(dirtyAddrs) == 0 {
		return nil
	}
	return k.incrementalUpdateTrustTree(ctx, dirtyAddrs)
}

// RebuildMemberTrustTree forces a full rebuild of the trust tree.
// Kept for backward compatibility; prefer MarkTrustTreeDirty + EndBlocker.
func (k Keeper) RebuildMemberTrustTree(ctx context.Context) error {
	k.clearAllTreeState(ctx)
	return k.initializeTrustTree(ctx)
}

// ---------------------------------------------------------------------------
// Root rotation
// ---------------------------------------------------------------------------

// setMemberTrustTreeRoot stores the current root and rotates the old current to previous.
func (k Keeper) setMemberTrustTreeRoot(ctx context.Context, root []byte) error {
	store := k.storeService.OpenKVStore(ctx)

	// Rotate: current -> previous.
	currentRoot, _ := store.Get(memberTrustTreeRootKey)
	if len(currentRoot) > 0 {
		if err := store.Set(previousMemberTrustTreeRootKey, currentRoot); err != nil {
			return err
		}
	}

	return store.Set(memberTrustTreeRootKey, root)
}

// ---------------------------------------------------------------------------
// Persistent tree node storage (sparse)
// ---------------------------------------------------------------------------

// nodeKey returns the KV store key for tree node at (level, index).
func nodeKey(level int, index uint64) []byte {
	key := make([]byte, len(trustTreeNodePrefix)+1+8)
	copy(key, trustTreeNodePrefix)
	key[len(trustTreeNodePrefix)] = byte(level)
	binary.BigEndian.PutUint64(key[len(trustTreeNodePrefix)+1:], index)
	return key
}

// getNode reads a tree node. Returns zeroHashes[level] if absent (sparse).
func (k Keeper) getNode(ctx context.Context, level int, index uint64) []byte {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(nodeKey(level, index))
	if err != nil || len(bz) == 0 {
		return zeroHashes[level]
	}
	return bz
}

// setNode writes a tree node. Deletes the key if the value equals the zero hash
// for that level (sparse storage optimization).
func (k Keeper) setNode(ctx context.Context, level int, index uint64, hash []byte) {
	store := k.storeService.OpenKVStore(ctx)
	key := nodeKey(level, index)
	if bytes.Equal(hash, zeroHashes[level]) {
		store.Delete(key) //nolint:errcheck
	} else {
		store.Set(key, hash) //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// Member ↔ leaf index mapping
// ---------------------------------------------------------------------------

func memberIdxKey(address string) []byte {
	return append(append([]byte(nil), trustTreeMemberIdxPrefix...), []byte(address)...)
}

func idxMemberKey(index uint64) []byte {
	key := make([]byte, len(trustTreeIdxMemberPrefix)+8)
	copy(key, trustTreeIdxMemberPrefix)
	binary.BigEndian.PutUint64(key[len(trustTreeIdxMemberPrefix):], index)
	return key
}

func dirtyMemberKey(address string) []byte {
	return append(append([]byte(nil), trustTreeDirtyMemberPrefix...), []byte(address)...)
}

// getMemberLeafIndex returns the leaf index for a member, or (0, false) if not assigned.
func (k Keeper) getMemberLeafIndex(ctx context.Context, address string) (uint64, bool) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(memberIdxKey(address))
	if err != nil || len(bz) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(bz), true
}

// setMemberLeafIndex stores the bidirectional mapping between member address and leaf index.
func (k Keeper) setMemberLeafIndex(ctx context.Context, address string, index uint64) {
	store := k.storeService.OpenKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, index)
	store.Set(memberIdxKey(address), bz)            //nolint:errcheck
	store.Set(idxMemberKey(index), []byte(address)) //nolint:errcheck
}

// allocLeafIndex returns the next available leaf index and increments the counter.
func (k Keeper) allocLeafIndex(ctx context.Context) uint64 {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(trustTreeNextLeafIdxKey)
	var idx uint64
	if err == nil && len(bz) == 8 {
		idx = binary.BigEndian.Uint64(bz)
	}
	next := make([]byte, 8)
	binary.BigEndian.PutUint64(next, idx+1)
	store.Set(trustTreeNextLeafIdxKey, next) //nolint:errcheck
	return idx
}

// peekNextLeafIndex returns the next available leaf index without incrementing.
func (k Keeper) peekNextLeafIndex(ctx context.Context) uint64 {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(trustTreeNextLeafIdxKey)
	if err != nil || len(bz) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

// ---------------------------------------------------------------------------
// Per-member dirty set
// ---------------------------------------------------------------------------

// getDirtyMembers returns all addresses in the dirty set, then clears it.
func (k Keeper) getDirtyMembers(ctx context.Context) []string {
	store := k.storeService.OpenKVStore(ctx)
	end := prefixEnd(trustTreeDirtyMemberPrefix)
	iter, err := store.Iterator(trustTreeDirtyMemberPrefix, end)
	if err != nil {
		return nil
	}
	defer iter.Close()

	var addresses []string
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		addr := string(key[len(trustTreeDirtyMemberPrefix):])
		addresses = append(addresses, addr)
	}

	// Clear dirty set.
	for _, addr := range addresses {
		store.Delete(dirtyMemberKey(addr)) //nolint:errcheck
	}
	return addresses
}

// ---------------------------------------------------------------------------
// Initialization & full-rebuild flags
// ---------------------------------------------------------------------------

func (k Keeper) isTrustTreeInitialized(ctx context.Context) bool {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(trustTreeInitializedKey)
	return err == nil && len(bz) > 0 && bz[0] == 1
}

func (k Keeper) setTrustTreeInitialized(ctx context.Context) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(trustTreeInitializedKey, []byte{1}) //nolint:errcheck
}

func (k Keeper) isFullRebuildRequested(ctx context.Context) bool {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(trustTreeDirtyKey)
	return err == nil && len(bz) > 0 && bz[0] == 1
}

func (k Keeper) clearFullRebuildFlag(ctx context.Context) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(trustTreeDirtyKey, []byte{0}) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Incremental path update — the core O(depth) algorithm
// ---------------------------------------------------------------------------

// updateLeafPath updates a single leaf and recomputes the path to the root.
// Returns the new root hash.
func (k Keeper) updateLeafPath(ctx context.Context, leafIndex uint64, newLeafHash []byte) []byte {
	k.setNode(ctx, 0, leafIndex, newLeafHash)

	currentIndex := leafIndex
	for level := 0; level < trustTreeDepth; level++ {
		leftIndex := currentIndex &^ 1 // clear lowest bit
		rightIndex := leftIndex | 1    // set lowest bit

		leftHash := k.getNode(ctx, level, leftIndex)
		rightHash := k.getNode(ctx, level, rightIndex)

		parentIndex := currentIndex >> 1
		parentHash := zkcrypto.HashTwoFields(leftHash, rightHash)

		k.setNode(ctx, level+1, parentIndex, parentHash)
		currentIndex = parentIndex
	}

	return k.getNode(ctx, trustTreeDepth, 0)
}

// ---------------------------------------------------------------------------
// First-time initialization: populate tree from all active voter-members
// ---------------------------------------------------------------------------

// initializeTrustTree populates the trust tree from all active members that
// have registered ZK public keys. Called on first EndBlocker or after full rebuild.
func (k Keeper) initializeTrustTree(ctx context.Context) error {
	iter, err := k.Member.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var root []byte
	leafCount := 0
	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}
		member := kv.Value
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
			continue
		}
		if len(member.ZkPublicKey) == 0 {
			continue
		}

		leafIndex := k.allocLeafIndex(ctx)
		k.setMemberLeafIndex(ctx, member.Address, leafIndex)

		trustLevel := uint64(member.TrustLevel)
		leaf := zkcrypto.ComputeLeaf(member.ZkPublicKey, trustLevel)
		root = k.updateLeafPath(ctx, leafIndex, leaf)
		leafCount++
	}

	if root != nil {
		if err := k.setMemberTrustTreeRoot(ctx, root); err != nil {
			return err
		}
	}

	// Only commit the initialized flag when we actually built a non-empty tree.
	// If leafCount == 0 (no members with ZK keys yet), leave initialized=false
	// so we retry on the next EndBlocker once keys are registered.
	if leafCount > 0 {
		k.setTrustTreeInitialized(ctx)
		k.clearFullRebuildFlag(ctx)
		k.getDirtyMembers(ctx) // discard — just clears the set
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.trust_tree.initialized",
		sdk.NewAttribute("leaf_count", fmt.Sprintf("%d", leafCount)),
	))

	return nil
}

// ---------------------------------------------------------------------------
// Full rebuild: clears all tree state and re-initializes
// ---------------------------------------------------------------------------

func (k Keeper) fullRebuildTrustTree(ctx context.Context) error {
	k.clearAllTreeState(ctx)
	return k.initializeTrustTree(ctx)
}

// clearAllTreeState removes all persistent tree nodes, member index mappings,
// and the leaf counter. Does NOT clear the root/prev-root (those are rotated).
func (k Keeper) clearAllTreeState(ctx context.Context) {
	store := k.storeService.OpenKVStore(ctx)

	// Clear nodes.
	clearPrefix(store, trustTreeNodePrefix)
	// Clear member-to-index and index-to-member mappings.
	clearPrefix(store, trustTreeMemberIdxPrefix)
	clearPrefix(store, trustTreeIdxMemberPrefix)
	// Clear dirty members.
	clearPrefix(store, trustTreeDirtyMemberPrefix)
	// Reset leaf counter.
	store.Delete(trustTreeNextLeafIdxKey) //nolint:errcheck
	// Clear initialized flag so initializeTrustTree runs.
	store.Delete(trustTreeInitializedKey) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Incremental update: process dirty members only
// ---------------------------------------------------------------------------

func (k Keeper) incrementalUpdateTrustTree(ctx context.Context, dirtyAddrs []string) error {
	var root []byte
	updatedCount := 0

	for _, addr := range dirtyAddrs {
		leafIndex, hasIndex := k.getMemberLeafIndex(ctx, addr)

		member, memberErr := k.Member.Get(ctx, addr)
		isActive := memberErr == nil && member.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE

		var newLeaf []byte

		if isActive {
			zkPubKey := k.getZkPubKeyForMember(ctx, addr)
			if zkPubKey == nil {
				// No ZK key registered — zero the leaf if one exists.
				if hasIndex {
					newLeaf = zeroHashes[0]
				} else {
					continue
				}
			} else {
				trustLevel := uint64(member.TrustLevel)
				newLeaf = zkcrypto.ComputeLeaf(zkPubKey, trustLevel)
				if !hasIndex {
					leafIndex = k.allocLeafIndex(ctx)
					k.setMemberLeafIndex(ctx, addr, leafIndex)
				}
			}
		} else {
			// Member zeroed/inactive — zero their leaf.
			if hasIndex {
				newLeaf = zeroHashes[0]
			} else {
				continue
			}
		}

		root = k.updateLeafPath(ctx, leafIndex, newLeaf)
		updatedCount++
	}

	if updatedCount > 0 && root != nil {
		if err := k.setMemberTrustTreeRoot(ctx, root); err != nil {
			return err
		}

		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.trust_tree.updated",
			sdk.NewAttribute("members_updated", fmt.Sprintf("%d", updatedCount)),
		))
	}

	return nil
}

// getZkPubKeyForMember looks up a single member's ZK public key from their
// Member record. Returns nil if the member has no ZK key registered.
func (k Keeper) getZkPubKeyForMember(ctx context.Context, addr string) []byte {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		return nil
	}
	if len(member.ZkPublicKey) == 0 {
		return nil
	}
	return member.ZkPublicKey
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// prefixEnd returns the end key for prefix iteration (prefix with last byte incremented).
// Returns nil if the prefix is empty or all 0xFF bytes.
func prefixEnd(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return nil // all 0xFF — unbounded
}

// clearPrefix deletes all keys with the given prefix from the store.
func clearPrefix(store corestore.KVStore, prefix []byte) {
	end := prefixEnd(prefix)
	iter, err := store.Iterator(prefix, end)
	if err != nil {
		return
	}
	defer iter.Close()

	var keys [][]byte
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, append([]byte(nil), iter.Key()...))
	}
	for _, key := range keys {
		store.Delete(key) //nolint:errcheck
	}
}

package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"

	"sparkdream/x/rep/types"
)

const subAddrKeyRecoveryDeposits = "recovery_deposits"

// RecoveryDepositEscrowAddress holds the SPARK deposits locked with sealed
// recovery commits until a reveal consumes the seal (refund) or it expires
// (burn).
func RecoveryDepositEscrowAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyRecoveryDeposits))
}

// validCommitment checks the shape of a commitment or sealed hash.
func validCommitment(c []byte) bool {
	return len(c) == types.RecoveryCommitmentSize
}

// SetRecoveryCommitment adds a recovery commitment to member's set,
// optionally replacing an existing one. Every commitment set by an existing
// member waits out recovery_change_delay before it becomes active, including
// the member's first: without the delay the recovery path would be a 2-day
// key path with no cooldown, cap or report check, and a thief holding the
// key could swap in their own commitment. Only `immediate` callers (the
// onboarding commitment carried by MsgAcceptInvitation) skip the delay: there
// is nothing to steal yet.
func (k Keeper) SetRecoveryCommitment(ctx context.Context, member string, commitment, replaces []byte, immediate bool) error {
	if !validCommitment(commitment) {
		return errorsmod.Wrapf(types.ErrRecoveryCommitmentInvalid, "commitment must be %d bytes", types.RecoveryCommitmentSize)
	}
	if k.IsTombstoned(ctx, member) {
		return types.ErrMemberTombstoned
	}
	m, err := k.Member.Get(ctx, member)
	if err != nil {
		return types.ErrMemberNotFound
	}
	if m.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	set, err := k.RecoveryCommitments.Get(ctx, member)
	if err != nil && !errorsmodIsNotFound(err) {
		return err
	}
	if set.FindCommitment(commitment) >= 0 {
		return types.ErrRecoveryCommitmentExists
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	effectiveAt := int64(0)
	if !immediate {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return err
		}
		effectiveAt = now + params.RecoveryChangeDelay
	}

	if len(replaces) > 0 {
		if set.FindCommitment(replaces) < 0 {
			return errorsmod.Wrap(types.ErrRecoveryCommitmentNotFound, "replaces")
		}
		scheduleRemoval(&set, replaces, effectiveAt)
	} else if len(set.Commitments) >= types.MaxRecoveryCommitments {
		return errorsmod.Wrapf(types.ErrRecoveryCommitmentCap, "max %d", types.MaxRecoveryCommitments)
	}

	set.Commitments = append(set.Commitments, types.RecoveryCommitment{Commitment: commitment, EffectiveAt: effectiveAt})
	if err := k.RecoveryCommitments.Set(ctx, member, set); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("recovery_commitment_set",
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("commitment", hex.EncodeToString(commitment)),
		sdk.NewAttribute("effective_at", fmt.Sprintf("%d", effectiveAt)),
		sdk.NewAttribute("replaces", hex.EncodeToString(replaces)),
	))
	return nil
}

// RemoveRecoveryCommitment schedules the removal of a commitment after
// recovery_change_delay. The commitment stays active until then, so a thief
// holding the key cannot strip the real owner's recovery on the spot.
func (k Keeper) RemoveRecoveryCommitment(ctx context.Context, member string, commitment []byte) error {
	if k.IsTombstoned(ctx, member) {
		return types.ErrMemberTombstoned
	}
	set, err := k.RecoveryCommitments.Get(ctx, member)
	if err != nil {
		return types.ErrRecoveryCommitmentNotFound
	}
	if set.FindCommitment(commitment) < 0 {
		return types.ErrRecoveryCommitmentNotFound
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	removalAt := sdkCtx.BlockTime().Unix() + params.RecoveryChangeDelay
	scheduleRemoval(&set, commitment, removalAt)
	if err := k.RecoveryCommitments.Set(ctx, member, set); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("recovery_commitment_removal_scheduled",
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("commitment", hex.EncodeToString(commitment)),
		sdk.NewAttribute("removal_at", fmt.Sprintf("%d", removalAt)),
	))
	return nil
}

// scheduleRemoval records (or re-times) a pending removal. A removal_at of 0
// removes at once.
func scheduleRemoval(set *types.RecoveryCommitmentSet, commitment []byte, removalAt int64) {
	if removalAt == 0 {
		idx := set.FindCommitment(commitment)
		if idx >= 0 {
			set.Commitments = append(set.Commitments[:idx], set.Commitments[idx+1:]...)
		}
		return
	}
	for i := range set.PendingRemovals {
		if bytes.Equal(set.PendingRemovals[i].Commitment, commitment) {
			set.PendingRemovals[i].RemovalAt = removalAt
			return
		}
	}
	set.PendingRemovals = append(set.PendingRemovals, types.PendingCommitmentRemoval{Commitment: commitment, RemovalAt: removalAt})
}

// compactCommitmentSet applies removals that have come due. Called before
// every read or write of a set, so removals are lazy and need no queue.
func compactCommitmentSet(set *types.RecoveryCommitmentSet, now int64) {
	kept := set.PendingRemovals[:0]
	for _, r := range set.PendingRemovals {
		if r.RemovalAt <= now {
			if idx := set.FindCommitment(r.Commitment); idx >= 0 {
				set.Commitments = append(set.Commitments[:idx], set.Commitments[idx+1:]...)
			}
			continue
		}
		kept = append(kept, r)
	}
	set.PendingRemovals = kept
}

// GetRecoveryCommitments returns member's commitment set with due removals
// applied (not persisted).
func (k Keeper) GetRecoveryCommitments(ctx context.Context, member string) (types.RecoveryCommitmentSet, bool) {
	set, err := k.RecoveryCommitments.Get(ctx, member)
	if err != nil {
		return types.RecoveryCommitmentSet{}, false
	}
	compactCommitmentSet(&set, sdk.UnwrapSDKContext(ctx).BlockTime().Unix())
	return set, true
}

// CommitRecovery stores a sealed commit and locks recovery_commit_deposit
// from the submitter. Anyone may submit one; the seal reveals nothing about
// the member, the preimage or the target.
func (k Keeper) CommitRecovery(ctx context.Context, submitter string, sealed []byte) error {
	if !validCommitment(sealed) {
		return errorsmod.Wrapf(types.ErrRecoverySealInvalid, "sealed must be %d bytes", types.RecoveryCommitmentSize)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if !params.MigrationEnabled {
		return types.ErrMigrationDisabled
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if existing, err := k.RecoveryCommits.Get(ctx, sealed); err == nil {
		if existing.ExpiresAt > now {
			return types.ErrRecoverySealExists
		}
		// Expired but not yet swept: burn its deposit now and let the new seal
		// take the slot.
		if err := k.expireRecoveryCommit(ctx, existing); err != nil {
			return err
		}
	}

	submitterAddr, err := k.addressCodec.StringToBytes(submitter)
	if err != nil {
		return errorsmod.Wrap(err, "invalid submitter address")
	}
	deposit := params.RecoveryCommitDeposit
	if deposit.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), deposit))
		if err := k.bankKeeper.SendCoins(ctx, submitterAddr, RecoveryDepositEscrowAddress(), coins); err != nil {
			return errorsmod.Wrap(types.ErrInsufficientBalance, err.Error())
		}
	}

	commit := types.RecoveryCommit{
		Sealed:    sealed,
		Submitter: submitter,
		Height:    sdkCtx.BlockHeight(),
		ExpiresAt: now + params.RecoveryCommitTtl,
		Deposit:   deposit,
	}
	if err := k.RecoveryCommits.Set(ctx, sealed, commit); err != nil {
		return err
	}
	if err := k.RecoveryCommitExpiry.Set(ctx, collections.Join(commit.ExpiresAt, sealed)); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("recovery_commit",
		sdk.NewAttribute("sealed", hex.EncodeToString(sealed)),
		sdk.NewAttribute("submitter", submitter),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", commit.ExpiresAt)),
	))
	return nil
}

// expireRecoveryCommit burns an expired seal's deposit and deletes it.
func (k Keeper) expireRecoveryCommit(ctx context.Context, commit types.RecoveryCommit) error {
	if commit.Deposit.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), commit.Deposit))
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, RecoveryDepositEscrowAddress(), types.ModuleName, coins); err != nil {
			return err
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
			return err
		}
	}
	if err := k.RecoveryCommits.Remove(ctx, commit.Sealed); err != nil {
		return err
	}
	if err := k.RecoveryCommitExpiry.Remove(ctx, collections.Join(commit.ExpiresAt, commit.Sealed)); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("recovery_commit_expired",
		sdk.NewAttribute("sealed", hex.EncodeToString(commit.Sealed)),
		sdk.NewAttribute("deposit_burned", commit.Deposit.String()),
	))
	return nil
}

// ExpireRecoveryCommits sweeps seals past their TTL, at most maxN per call.
func (k Keeper) ExpireRecoveryCommits(ctx context.Context, now int64, maxN int) error {
	var due [][]byte
	rng := new(collections.Range[collections.Pair[int64, []byte]]).
		EndExclusive(collections.Join[int64, []byte](now+1, nil))
	err := k.RecoveryCommitExpiry.Walk(ctx, rng, func(key collections.Pair[int64, []byte]) (bool, error) {
		if key.K1() > now {
			return true, nil
		}
		due = append(due, key.K2())
		return len(due) >= maxN, nil
	})
	if err != nil {
		return err
	}
	for _, sealed := range due {
		commit, err := k.RecoveryCommits.Get(ctx, sealed)
		if err != nil {
			continue
		}
		if err := k.expireRecoveryCommit(ctx, commit); err != nil {
			return err
		}
	}
	return nil
}

// RevealRecovery opens a RECOVERY migration for member to newAddress on proof
// of a preimage. Valid iff the preimage hashes to one of member's active
// commitments, a sealed commit binding (preimage, member, newAddress) exists
// and has aged recovery_reveal_min_blocks, member is not zeroed and
// newAddress is fresh. Any account may be the revealer; the seal already
// binds the target, so redirection is impossible, and keeping the preimage
// off a multisig's signature-collection path is the point (plan §4.2).
//
// The matched commitment is consumed here, not at accept: the preimage is
// public from this block on. Every pending commitment change is voided, the
// seal's deposit is refunded, and any pending KEY or COUNCIL migration is
// cancelled (RECOVERY outranks both).
func (k Keeper) RevealRecovery(ctx context.Context, revealer, member, newAddress string, preimage []byte) (uint64, error) {
	if len(preimage) == 0 || len(preimage) > types.MaxRecoveryPreimageSize {
		return 0, errorsmod.Wrapf(types.ErrRecoveryCommitmentInvalid, "preimage must be 1..%d bytes", types.MaxRecoveryPreimageSize)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	if !params.MigrationEnabled {
		return 0, types.ErrMigrationDisabled
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	set, found := k.GetRecoveryCommitments(ctx, member)
	if !found {
		return 0, types.ErrRecoveryCommitmentInactive
	}
	want := types.RecoveryCommitmentHash(preimage)
	idx := set.FindCommitment(want)
	if idx < 0 || !set.ActiveCommitment(set.Commitments[idx], now) {
		return 0, types.ErrRecoveryCommitmentInactive
	}

	sealed := types.RecoverySeal(preimage, member, newAddress)
	commit, err := k.RecoveryCommits.Get(ctx, sealed)
	if err != nil {
		return 0, types.ErrRecoverySealNotFound
	}
	if commit.ExpiresAt <= now {
		return 0, types.ErrRecoverySealExpired
	}
	if sdkCtx.BlockHeight()-commit.Height < params.RecoveryRevealMinBlocks {
		return 0, errorsmod.Wrapf(types.ErrRecoverySealTooYoung, "sealed at height %d, %d blocks required", commit.Height, params.RecoveryRevealMinBlocks)
	}

	// Open the migration first: it runs every freshness and zeroing check and
	// cancels lower-precedence migrations. Nothing below is reached if it
	// refuses, so a failed reveal does not consume the commitment on chain
	// (the preimage is still burned by publicity; wallets reveal against
	// another commitment).
	id, err := k.openMigration(ctx, member, newAddress, types.MigrationPath_MIGRATION_PATH_RECOVERY, "")
	if err != nil {
		return 0, err
	}

	// Consume the commitment and void every pending change: a reveal against
	// an active commitment outranks whatever the key holder scheduled.
	set.Commitments = append(set.Commitments[:idx], set.Commitments[idx+1:]...)
	active := set.Commitments[:0]
	for _, c := range set.Commitments {
		if c.EffectiveAt <= now {
			active = append(active, c)
		}
	}
	set.Commitments = active
	set.PendingRemovals = nil
	if err := k.RecoveryCommitments.Set(ctx, member, set); err != nil {
		return 0, err
	}

	// Refund the seal deposit and drop the seal.
	if commit.Deposit.IsPositive() {
		submitterAddr, err := k.addressCodec.StringToBytes(commit.Submitter)
		if err == nil {
			coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), commit.Deposit))
			if err := k.bankKeeper.SendCoins(ctx, RecoveryDepositEscrowAddress(), submitterAddr, coins); err != nil {
				return 0, err
			}
		}
	}
	if err := k.RecoveryCommits.Remove(ctx, sealed); err != nil {
		return 0, err
	}
	if err := k.RecoveryCommitExpiry.Remove(ctx, collections.Join(commit.ExpiresAt, sealed)); err != nil {
		return 0, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("recovery_revealed",
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("new_address", newAddress),
		sdk.NewAttribute("revealer", revealer),
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("commitment", hex.EncodeToString(want)),
	))
	return id, nil
}

func errorsmodIsNotFound(err error) bool {
	return errors.Is(err, collections.ErrNotFound)
}

package dreamutil

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// DREAMKeeper is the shared interface for DREAM token operations.
// Both x/name and x/season RepKeeper interfaces include these methods.
type DREAMKeeper interface {
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// BurnLockedDREAM releases and burns one escrowed amount in a single
	// call, so a failure leaves the lock in place.
	BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
}

// Ops provides DREAM operations with string address convenience methods.
// Modules embed this in their keeper to avoid duplicating address conversion boilerplate.
type Ops struct {
	keeper       DREAMKeeper
	addressCodec address.Codec
}

// NewOps creates a new Ops instance. If keeper is nil, all operations are no-ops
// (development mode where x/rep is not wired up).
func NewOps(keeper DREAMKeeper, addressCodec address.Codec) Ops {
	return Ops{keeper: keeper, addressCodec: addressCodec}
}

// Lock escrows DREAM tokens for the given address.
func (o Ops) Lock(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.LockDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// Unlock releases escrowed DREAM tokens for the given address.
func (o Ops) Unlock(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.UnlockDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// Burn destroys DREAM tokens from the given address's UNLOCKED balance (a
// fee). x/rep refuses to burn locked DREAM through this path; slashing an
// escrowed stake goes through BurnLocked.
func (o Ops) Burn(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.BurnDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// BurnLocked slashes an escrowed stake: it releases the stake's lock and
// burns the same amount through x/rep's BurnLockedDREAM, in one call, so a
// failed burn never leaves the stake unlocked. Burning a locked stake with
// Burn alone left it locked forever (and paid the burn out of the holder's
// free balance, or failed when there was none).
func (o Ops) BurnLocked(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.BurnLockedDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// SettleStakes handles the common dispute resolution pattern:
// the winner's stake is unlocked (returned) and the loser's stake is burned.
func (o Ops) SettleStakes(ctx context.Context, winnerAddr string, winnerAmount uint64, loserAddr string, loserAmount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	if err := o.Unlock(ctx, winnerAddr, winnerAmount); err != nil {
		return err
	}
	return o.BurnLocked(ctx, loserAddr, loserAmount)
}

// IsLockGone reports whether err is x/rep's answer for a lock that no longer
// exists: the holder was zeroed (ZeroMember burned the staked DREAM and reset
// StakedDream, so an unlock fails with ErrInsufficientStake) or is not a
// member at all.
func IsLockGone(err error) bool {
	return errors.Is(err, reptypes.ErrInsufficientStake) || errors.Is(err, reptypes.ErrMemberNotFound)
}

// UnlockOrGone is Unlock for a module's own lock record that may have
// outlived its DREAM. It returns (false, nil) when x/rep reports the lock as
// already gone (IsLockGone), so the caller can settle the record instead of
// failing every path that releases it; any other error is returned.
//
// It is a fallback, not the cleanup: a module holding lock records drops a
// zeroed member's records in its MemberZeroingHook. A record left behind
// would unlock DREAM of an unrelated lock once an overturned zeroing gave
// the member a balance back (REV-101), and this check cannot tell the two
// apart.
func (o Ops) UnlockOrGone(ctx context.Context, addr string, amount uint64) (bool, error) {
	if amount == 0 {
		return false, nil
	}
	if err := o.Unlock(ctx, addr, amount); err != nil {
		if IsLockGone(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// BurnLockedOrGone is BurnLocked with the same "lock already gone"
// tolerance as UnlockOrGone.
func (o Ops) BurnLockedOrGone(ctx context.Context, addr string, amount uint64) (bool, error) {
	if amount == 0 {
		return false, nil
	}
	if err := o.BurnLocked(ctx, addr, amount); err != nil {
		if IsLockGone(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SettleStakesOrGone is SettleStakes built on UnlockOrGone and
// BurnLockedOrGone: a side whose lock is already gone is treated as settled.
func (o Ops) SettleStakesOrGone(ctx context.Context, winnerAddr string, winnerAmount uint64, loserAddr string, loserAmount uint64) error {
	if _, err := o.UnlockOrGone(ctx, winnerAddr, winnerAmount); err != nil {
		return err
	}
	_, err := o.BurnLockedOrGone(ctx, loserAddr, loserAmount)
	return err
}

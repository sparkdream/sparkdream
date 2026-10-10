package keeper

import (
	"context"
	"errors"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

// GetParams returns the current params (defaults if unset).
func (k Keeper) GetParams(ctx context.Context) types.Params {
	p, err := k.Params.Get(ctx)
	if err != nil {
		return types.DefaultParams()
	}
	return p
}

func now(ctx context.Context) int64 {
	return sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
}

func height(ctx context.Context) int64 {
	return sdk.UnwrapSDKContext(ctx).BlockHeight()
}

func emit(ctx context.Context, eventType string, attrs ...sdk.Attribute) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }
func i64(v int64) string  { return strconv.FormatInt(v, 10) }

func classAttr(id uint64) sdk.Attribute { return sdk.NewAttribute("class_id", u64(id)) }
func tokenAttr(id uint64) sdk.Attribute { return sdk.NewAttribute("token_id", u64(id)) }

// ---------------------------------------------------------------------------
// Addresses
// ---------------------------------------------------------------------------

func (k Keeper) decodeAddr(field, addr string) (sdk.AccAddress, error) {
	bz, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidRecipient, "invalid %s address: %v", field, err)
	}
	return bz, nil
}

// validateRecipient decodes addr and rejects bank-blocked (module) addresses,
// which would strand tokens or funds (docs/x-artifact-spec.md §5, recipient rule).
func (k Keeper) validateRecipient(field, addr string) (sdk.AccAddress, error) {
	bz, err := k.decodeAddr(field, addr)
	if err != nil {
		return nil, err
	}
	if k.bankKeeper.BlockedAddr(bz) || bz.Equals(k.ModuleAddress()) {
		return nil, errorsmod.Wrapf(types.ErrInvalidRecipient, "%s %s is a blocked module address", field, addr)
	}
	return bz, nil
}

// validateRoyaltyShares checks a royalty split's shape and that every
// recipient passes the recipient rule.
func (k Keeper) validateRoyaltyShares(shares []types.RoyaltyShare) error {
	if err := types.ValidateRoyaltyShares(shares); err != nil {
		return err
	}
	for _, s := range shares {
		if _, err := k.validateRecipient("royalty_recipients", s.Address); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Coins
// ---------------------------------------------------------------------------

// validatePrice enforces the coin rule: bond denom only, DREAM rejected loudly.
func (k Keeper) validatePrice(ctx context.Context, c sdk.Coin, allowZero bool) error {
	if dream := k.DreamDenom(ctx); dream != "" && c.Denom == dream {
		return types.ErrDreamNotAccepted
	}
	if c.Denom != k.BondDenom(ctx) {
		return errorsmod.Wrapf(types.ErrInvalidDenom, "expected %s, got %q", k.BondDenom(ctx), c.Denom)
	}
	if c.Amount.IsNil() || c.Amount.IsNegative() {
		return errorsmod.Wrap(types.ErrInvalidDenom, "amount must be non-negative")
	}
	if !allowZero && c.Amount.IsZero() {
		return errorsmod.Wrap(types.ErrInvalidDenom, "amount must be positive")
	}
	return nil
}

func (k Keeper) coins(ctx context.Context, amt math.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amt))
}

// collect moves amt of the bond denom from addr into the module account.
func (k Keeper) collect(ctx context.Context, from sdk.AccAddress, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, k.coins(ctx, amt)); err != nil {
		return errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
	}
	return nil
}

// refund moves amt of the bond denom from the module account to addr.
func (k Keeper) refund(ctx context.Context, to string, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	addr, err := k.decodeAddr("refund", to)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, k.coins(ctx, amt))
}

// burnFrom collects amt from addr and burns it.
func (k Keeper) burnFrom(ctx context.Context, from sdk.AccAddress, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	if err := k.collect(ctx, from, amt); err != nil {
		return err
	}
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, k.coins(ctx, amt))
}

// burnHeld burns amt already held by the module account.
func (k Keeper) burnHeld(ctx context.Context, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, k.coins(ctx, amt))
}

// send pays amt directly between accounts (sale settlement).
func (k Keeper) send(ctx context.Context, from sdk.AccAddress, to string, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	toAddr, err := k.decodeAddr("payee", to)
	if err != nil {
		return err
	}
	if err := k.bankKeeper.SendCoins(ctx, from, toAddr, k.coins(ctx, amt)); err != nil {
		return errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
	}
	return nil
}

// fundCommunityPool routes a sale fee to the community pool. Without a wired
// distribution keeper (unit tests) the fee is burned instead, so funds are
// never silently kept by the module.
func (k Keeper) fundCommunityPool(ctx context.Context, from sdk.AccAddress, amt math.Int) error {
	if !amt.IsPositive() {
		return nil
	}
	if k.late.distr == nil {
		return k.burnFrom(ctx, from, amt)
	}
	if err := k.late.distr.FundCommunityPool(ctx, k.coins(ctx, amt), from); err != nil {
		return errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

func (k Keeper) isCouncilBody(ctx context.Context, addr string) bool {
	return k.late.commons != nil && k.late.commons.IsCouncilBodyPolicy(ctx, addr)
}

func (k Keeper) isActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	return k.late.rep != nil && k.late.rep.IsActiveMember(ctx, addr)
}

// isTrustedSender: an active x/rep member or a council-body policy (§7.6).
func (k Keeper) isTrustedSender(ctx context.Context, addr string) bool {
	if k.isCouncilBody(ctx, addr) {
		return true
	}
	bz, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return k.isActiveMember(ctx, bz)
}

// canCreateClass: council body, or active member at the required trust level.
func (k Keeper) canCreateClass(ctx context.Context, addr sdk.AccAddress, addrStr string, p types.Params) bool {
	if k.isCouncilBody(ctx, addrStr) {
		return true
	}
	if !k.isActiveMember(ctx, addr) {
		return false
	}
	lvl, err := k.late.rep.GetTrustLevel(ctx, addr)
	if err != nil {
		return false
	}
	return uint32(lvl) >= p.MinTrustLevelCreateClass
}

// isOpsAuthority: gov, the Commons Council policy, the Ops Committee policy,
// or an individual Ops Committee member (§5.6).
func (k Keeper) isOpsAuthority(ctx context.Context, addr string) bool {
	if bz, err := k.addressCodec.StringToBytes(addr); err == nil && string(bz) == string(k.authority) {
		return true
	}
	return k.late.commons != nil && k.late.commons.IsCouncilAuthorized(ctx, addr, "commons", "operations")
}

// ---------------------------------------------------------------------------
// Receive policy
// ---------------------------------------------------------------------------

// EffectiveReceivePolicy returns the policy for addr and whether it is the default.
func (k Keeper) EffectiveReceivePolicy(ctx context.Context, addr string) (types.ReceivePolicy, bool) {
	v, err := k.ReceivePolicies.Get(ctx, addr)
	if err == nil && v != 0 {
		return types.ReceivePolicy(v), false
	}
	return k.GetParams(ctx).DefaultReceivePolicy, true
}

// deliversDirectly resolves the recipient's policy for a sender (§7.6).
func (k Keeper) deliversDirectly(ctx context.Context, sender, recipient string) bool {
	policy, _ := k.EffectiveReceivePolicy(ctx, recipient)
	switch policy {
	case types.ReceivePolicy_RECEIVE_POLICY_OPEN:
		return true
	case types.ReceivePolicy_RECEIVE_POLICY_INBOX:
		return false
	default: // MEMBERS
		return k.isTrustedSender(ctx, sender)
	}
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

func (k Keeper) getClass(ctx context.Context, id uint64) (types.Class, error) {
	c, err := k.Classes.Get(ctx, id)
	if errors.Is(err, collections.ErrNotFound) {
		return c, errorsmod.Wrapf(types.ErrClassNotFound, "class %d", id)
	}
	return c, err
}

func (k Keeper) getOwnedClass(ctx context.Context, id uint64, owner string) (types.Class, error) {
	c, err := k.getClass(ctx, id)
	if err != nil {
		return c, err
	}
	if c.Owner != owner {
		return c, types.ErrNotClassOwner
	}
	return c, nil
}

func (k Keeper) getToken(ctx context.Context, classID, tokenID uint64) (types.Token, error) {
	t, err := k.Tokens.Get(ctx, collections.Join(classID, tokenID))
	if errors.Is(err, collections.ErrNotFound) {
		return t, errorsmod.Wrapf(types.ErrTokenNotFound, "token %d/%d", classID, tokenID)
	}
	return t, err
}

// isTargetHidden reports whether a token or its class is hidden.
func isTargetHidden(c types.Class, t types.Token) bool {
	return c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN || t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN
}

func checkBatch(n int, p types.Params) error {
	if n == 0 {
		return errorsmod.Wrap(types.ErrInvalidBatch, "batch is empty")
	}
	if uint32(n) > p.MaxBatchSize {
		return errorsmod.Wrapf(types.ErrInvalidBatch, "batch exceeds %d entries", p.MaxBatchSize)
	}
	return nil
}

// checkNoDuplicateRefs rejects a batch naming the same token twice.
// (collections.Pair holds pointers, so it cannot be a map key.)
func checkNoDuplicateRefs(refs []U64Pair) error {
	seen := make(map[[2]uint64]bool, len(refs))
	for _, r := range refs {
		k := [2]uint64{r.K1(), r.K2()}
		if seen[k] {
			return errorsmod.Wrapf(types.ErrInvalidBatch, "duplicate token %d/%d", r.K1(), r.K2())
		}
		seen[k] = true
	}
	return nil
}

var sentinelRole = reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL

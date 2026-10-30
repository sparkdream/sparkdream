package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	sessiontypes "sparkdream/x/session/types"
	shieldtypes "sparkdream/x/shield/types"
	"sparkdream/x/vault/types"
)

// validateSigners runs the shared config rules and refuses non-personal
// signer addresses (vaults, module accounts, group policies, reserved
// addresses). Native multisig addresses are allowed: they cannot be detected
// and cause no harm. It also refuses tombstoned signers (an address that has
// migrated away can never approve) and two entries that resolve to the same
// canonical member (one approval would count both weights while the other
// signers see two people), and caps how many vaults one member may sign
// (MaxVaultsPerSigner distinct vaults across every x/rep alias of the
// signer, counting every vault but vaultID; 0 on create). The cap applies to
// accepted entries only: an unaccepted listing is checked again when its
// address accepts it (AcceptSigner).
func (k Keeper) validateSigners(ctx context.Context, vaultID uint64, signers []types.VaultSigner, threshold uint64, ttl int64, label string) error {
	if err := types.ValidateConfig(signers, threshold, ttl, label); err != nil {
		return err
	}
	canon := make(map[string]string, len(signers))
	for _, s := range signers {
		if _, err := k.addressCodec.StringToBytes(s.Address); err != nil {
			return errorsmod.Wrapf(types.ErrInvalidSignerSet, "signer %s: %v", s.Address, err)
		}
		if k.IsVault(ctx, s.Address) {
			return errorsmod.Wrapf(types.ErrNotPersonalAddress, "%s is a vault", s.Address)
		}
		if k.late.rep != nil {
			if err := k.late.rep.IsPersonalAddress(ctx, s.Address); err != nil {
				return errorsmod.Wrapf(types.ErrNotPersonalAddress, "%s: %v", s.Address, err)
			}
			if k.late.rep.IsTombstoned(ctx, s.Address) {
				return errorsmod.Wrapf(types.ErrNotPersonalAddress, "%s has migrated away", s.Address)
			}
		}
		c := k.canonical(ctx, s.Address)
		if prev, dup := canon[c]; dup {
			return errorsmod.Wrapf(types.ErrInvalidSignerSet, "%s and %s are the same member", prev, s.Address)
		}
		canon[c] = s.Address
		if !s.Accepted {
			continue
		}
		n, err := k.countVaultsOfSigner(ctx, s.Address, vaultID)
		if err != nil {
			return err
		}
		if n >= types.MaxVaultsPerSigner {
			return errorsmod.Wrapf(types.ErrTooManyVaultsForSigner, "%s already signs %d vaults", s.Address, n)
		}
	}
	return nil
}

// countVaultsOfSigner counts the distinct vaults, other than exclude, that
// list addr or any x/rep alias of it as an accepted signer. Signer entries are never
// re-keyed on migration, so a per-address count would let a member who
// migrates collect MaxVaultsPerSigner fresh entries under every new address.
// Stops at MaxVaultsPerSigner: at most MaxLifetimeAliases + 1 prefix walks and
// MaxVaultsPerSigner + 1 counted entries.
func (k Keeper) countVaultsOfSigner(ctx context.Context, addr string, exclude uint64) (int, error) {
	seen := make(map[uint64]struct{}, types.MaxVaultsPerSigner)
	for _, a := range k.allAddressesOf(ctx, addr) {
		rng := collections.NewPrefixedPairRange[string, uint64](a)
		err := k.VaultsBySigner.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			if key.K2() != exclude {
				seen[key.K2()] = struct{}{}
			}
			return len(seen) >= types.MaxVaultsPerSigner, nil
		})
		if err != nil {
			return 0, err
		}
		if len(seen) >= types.MaxVaultsPerSigner {
			break
		}
	}
	return len(seen), nil
}

// withAcceptance returns a copy of signers with the accepted flag set by the
// chain (any value on input is ignored). An entry is accepted when the
// listing address consented already: it is the creator's own entry (alias
// aware), or it carries over an accepted entry of the previous config with
// the same address or the same canonical member (a vault may re-key a
// migrated signer to its current address without asking again). Everything
// else starts unaccepted.
func (k Keeper) withAcceptance(ctx context.Context, signers []types.VaultSigner, creator string, prev []types.VaultSigner) []types.VaultSigner {
	prevAccepted := make(map[string]bool, len(prev)*2)
	for _, p := range prev {
		if p.Accepted {
			prevAccepted[p.Address] = true
			prevAccepted[k.canonical(ctx, p.Address)] = true
		}
	}
	out := make([]types.VaultSigner, len(signers))
	for i, s := range signers {
		s.Accepted = false
		switch {
		case creator != "" && k.canActAs(ctx, creator, s.Address):
			s.Accepted = true
		case prevAccepted[s.Address] || prevAccepted[k.canonical(ctx, s.Address)]:
			s.Accepted = true
		}
		out[i] = s
	}
	return out
}

// signerIndex returns the index an entry lives in: VaultsBySigner when
// accepted, VaultInvitesBySigner otherwise.
func (k Keeper) signerIndex(s types.VaultSigner) collections.KeySet[collections.Pair[string, uint64]] {
	if s.Accepted {
		return k.VaultsBySigner
	}
	return k.VaultInvitesBySigner
}

// indexSigners writes the index entry of every signer of vault id.
func (k Keeper) indexSigners(ctx context.Context, id uint64, signers []types.VaultSigner) error {
	for _, s := range signers {
		if err := k.signerIndex(s).Set(ctx, collections.Join(s.Address, id)); err != nil {
			return err
		}
	}
	return nil
}

// unindexSigners removes the index entry of every signer of vault id.
func (k Keeper) unindexSigners(ctx context.Context, id uint64, signers []types.VaultSigner) error {
	for _, s := range signers {
		if err := k.signerIndex(s).Remove(ctx, collections.Join(s.Address, id)); err != nil {
			return err
		}
	}
	return nil
}

// AcceptedWeight is the weight the vault's accepted entries can approve
// with, each member counted once at its largest accepted entry weight (the
// Tally of a proposal every accepted signer approved). Proposals cannot
// execute while it is below the threshold.
func (k Keeper) AcceptedWeight(ctx context.Context, signers []types.VaultSigner) uint64 {
	best := make(map[string]uint64, len(signers))
	for _, s := range signers {
		if !s.Accepted {
			continue
		}
		c := k.canonical(ctx, s.Address)
		if s.Weight > best[c] {
			best[c] = s.Weight
		}
	}
	var sum uint64
	for _, w := range best {
		sum += w
	}
	return sum
}

// CreateVault burns the creation fee from creator and stores a new vault.
// The creator's own entry (if listed) is accepted; every other entry waits
// for MsgAcceptVaultSigner.
func (k Keeper) CreateVault(ctx context.Context, creator sdk.AccAddress, signers []types.VaultSigner, threshold uint64, ttl int64, label string) (types.Vault, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Vault{}, err
	}
	if !params.CreationEnabled {
		return types.Vault{}, types.ErrCreationDisabled
	}
	creatorStr, err := k.addressCodec.BytesToString(creator)
	if err != nil {
		return types.Vault{}, err
	}
	signers = k.withAcceptance(ctx, signers, creatorStr, nil)
	if err := k.validateSigners(ctx, 0, signers, threshold, ttl, label); err != nil {
		return types.Vault{}, err
	}
	if params.CreationFeeAmount.IsPositive() {
		fee := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), params.CreationFeeAmount))
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, fee); err != nil {
			return types.Vault{}, errorsmod.Wrap(err, "creation fee")
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, fee); err != nil {
			return types.Vault{}, err
		}
	}

	id, err := k.VaultSeq.Next(ctx)
	if err != nil {
		return types.Vault{}, err
	}
	id++ // ids start at 1 so a zero id is never a valid reference
	addrBytes := types.VaultAddress(id)
	addr, err := k.addressCodec.BytesToString(addrBytes)
	if err != nil {
		return types.Vault{}, err
	}
	// Bank auto-creates a BaseAccount when someone sends to an address first;
	// accept an existing keyless base account, refuse anything else.
	if acc := k.authKeeper.GetAccount(ctx, addrBytes); acc != nil {
		base, ok := acc.(*authtypes.BaseAccount)
		if !ok || base.GetPubKey() != nil {
			return types.Vault{}, errorsmod.Wrapf(types.ErrAddressOccupied, "%s", addr)
		}
	} else {
		k.authKeeper.SetAccount(ctx, k.authKeeper.NewAccountWithAddress(ctx, addrBytes))
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	v := types.Vault{
		VaultId:       id,
		Address:       addr,
		Signers:       signers,
		Threshold:     threshold,
		ConfigVersion: 1,
		ProposalTtl:   ttl,
		Label:         label,
		CreatedAt:     sdkCtx.BlockTime().Unix(),
	}
	if err := k.Vault.Set(ctx, id, v); err != nil {
		return types.Vault{}, err
	}
	if err := k.VaultByAddress.Set(ctx, addr, id); err != nil {
		return types.Vault{}, err
	}
	if err := k.indexSigners(ctx, id, signers); err != nil {
		return types.Vault{}, err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("vault_created",
		sdk.NewAttribute("vault_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("address", addr),
		sdk.NewAttribute("creator", creator.String()),
		sdk.NewAttribute("threshold", fmt.Sprintf("%d", threshold)),
	))
	return v, nil
}

// signerOf resolves signer against the vault's accepted entries through the
// alias contract and returns the stored signer address and weight (0 = not a
// signer). An unaccepted entry cannot propose, approve, revoke or execute.
func (k Keeper) signerOf(ctx context.Context, v types.Vault, signer string) (string, uint64) {
	for _, s := range v.Signers {
		if s.Accepted && k.canActAs(ctx, signer, s.Address) {
			return s.Address, s.Weight
		}
	}
	return "", 0
}

// Tally sums the weight of approvers who are current accepted signers,
// matched by canonical address (an unaccepted entry has no weight). Each
// member counts once, at the largest weight among its entries: two entries can only resolve to the same member after a
// RECOVERY or COUNCIL migration into an address that already co-signed
// (validateSigners refuses it on every write, and the migration hook on the
// KEY path), and one person's approval must never count twice.
func (k Keeper) Tally(ctx context.Context, v types.Vault, p types.VaultProposal) uint64 {
	best := make(map[string]uint64, len(v.Signers))
	for _, s := range v.Signers {
		if !s.Accepted {
			continue
		}
		canon := k.canonical(ctx, s.Address)
		for _, a := range p.Approvals {
			if a == canon || a == s.Address {
				if s.Weight > best[canon] {
					best[canon] = s.Weight
				}
				break
			}
		}
	}
	var sum uint64
	for _, w := range best {
		sum += w
	}
	return sum
}

// checkInnerMsgs unpacks every inner message, checks the denylist and that
// each one is signed by exactly the vault address. Returns the unpacked msgs.
func (k Keeper) checkInnerMsgs(ctx context.Context, v types.Vault, anys []*codectypes.Any) ([]sdk.Msg, error) {
	if len(anys) == 0 {
		return nil, types.ErrNoMessages
	}
	if len(anys) > types.MaxMessagesPerProposal {
		return nil, errorsmod.Wrapf(types.ErrTooManyMessages, "at most %d, got %d", types.MaxMessagesPerProposal, len(anys))
	}
	vaultBytes, err := k.addressCodec.StringToBytes(v.Address)
	if err != nil {
		return nil, err
	}
	// The gnovm ante refuses a tx mixing VM messages with any other
	// message; a vault proposal's messages execute as one unit and skip the
	// ante, so apply the same rule here.
	gno := 0
	for _, a := range anys {
		if a != nil && commontypes.IsGnoVMMsgTypeURL(a.TypeUrl) {
			gno++
		}
	}
	if gno > 0 && gno != len(anys) {
		return nil, errorsmod.Wrap(types.ErrInnerMsgDenied, "cannot mix GnoVM messages with non-GnoVM messages in one proposal")
	}
	msgs := make([]sdk.Msg, 0, len(anys))
	for i, a := range anys {
		if a == nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "message %d is empty", i)
		}
		if err := k.checkDenylist(a.TypeUrl); err != nil {
			return nil, errorsmod.Wrapf(err, "message %d", i)
		}
		var m sdk.Msg
		if err := k.cdc.UnpackAny(a, &m); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "message %d: %v", i, err)
		}
		signers, _, err := k.cdc.GetMsgV1Signers(m)
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "message %d: signers: %v", i, err)
		}
		if len(signers) != 1 || string(signers[0]) != string(vaultBytes) {
			return nil, errorsmod.Wrapf(types.ErrInnerSignerMismatch, "message %d (%s)", i, a.TypeUrl)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// checkDenylist refuses types a vault must never execute: rep's identity
// messages (a vault has no personhood), anonymous execution, session exec (a
// vault may grant but never exec as a grantee) and every x/vault message
// except MsgUpdateVault / MsgCancelProposal (no vault-in-vault recursion).
func (k Keeper) checkDenylist(url string) error {
	for _, u := range reptypes.IdentityMsgTypeURLs() {
		if u == url {
			return errorsmod.Wrapf(types.ErrInnerMsgDenied, "%s: identity message", url)
		}
	}
	if url == sdk.MsgTypeURL(&shieldtypes.MsgShieldedExec{}) {
		return errorsmod.Wrapf(types.ErrInnerMsgDenied, "%s: anonymous execution", url)
	}
	if url == sdk.MsgTypeURL(&sessiontypes.MsgExecSession{}) {
		return errorsmod.Wrapf(types.ErrInnerMsgDenied, "%s: session execution", url)
	}
	// The commons proposal-fee floor is enforced only in the ante, which a
	// vault proposal's inner messages skip.
	if url == commontypes.CommonsSubmitProposalTypeURL {
		return errorsmod.Wrapf(types.ErrInnerMsgDenied, "%s: proposal fee is ante-only", url)
	}
	for _, u := range types.NonDelegableMsgTypeURLs() {
		if u == url && !types.IsInnerAllowedVaultMsg(url) {
			return errorsmod.Wrapf(types.ErrInnerMsgDenied, "%s: vault recursion", url)
		}
	}
	return nil
}

// isUpdateOnly reports whether the proposal's only message is MsgUpdateVault.
func isUpdateOnly(anys []*codectypes.Any) bool {
	return len(anys) == 1 && anys[0] != nil && anys[0].TypeUrl == sdk.MsgTypeURL(&types.MsgUpdateVault{})
}

// PendingCounts counts the proposer's PENDING proposals in the vault, split
// into update-only and the rest, by scanning the proposer's bounded index.
func (k Keeper) PendingCounts(ctx context.Context, vaultID uint64, proposer string) (general, updateOnly int, err error) {
	rng := collections.NewSuperPrefixedTripleRange[uint64, string, uint64](vaultID, proposer)
	err = k.PendingByProposer.Walk(ctx, rng, func(key collections.Triple[uint64, string, uint64]) (bool, error) {
		p, err := k.VaultProposal.Get(ctx, key.K3())
		if err != nil {
			return false, nil
		}
		if isUpdateOnly(p.Messages) {
			updateOnly++
		} else {
			general++
		}
		return false, nil
	})
	return general, updateOnly, err
}

// Propose opens a proposal; the proposer approves implicitly.
func (k Keeper) Propose(ctx context.Context, signer string, vaultID uint64, anys []*codectypes.Any, memo string) (types.VaultProposal, error) {
	v, err := k.Vault.Get(ctx, vaultID)
	if err != nil {
		return types.VaultProposal{}, errorsmod.Wrapf(types.ErrVaultNotFound, "%d", vaultID)
	}
	stored, _ := k.signerOf(ctx, v, signer)
	if stored == "" {
		return types.VaultProposal{}, types.ErrNotSigner
	}
	if len(memo) > types.MaxMemoLength {
		return types.VaultProposal{}, errorsmod.Wrapf(types.ErrInvalidMemo, "at most %d chars", types.MaxMemoLength)
	}
	if _, err := k.checkInnerMsgs(ctx, v, anys); err != nil {
		return types.VaultProposal{}, err
	}
	proposer := k.canonical(ctx, stored)
	general, updateOnly, err := k.PendingCounts(ctx, vaultID, proposer)
	if err != nil {
		return types.VaultProposal{}, err
	}
	if isUpdateOnly(anys) {
		if updateOnly >= types.MaxPendingUpdateProposalsPerProposer {
			return types.VaultProposal{}, errorsmod.Wrapf(types.ErrTooManyPending, "%d update proposals pending", updateOnly)
		}
	} else if general >= types.MaxPendingProposalsPerProposer {
		return types.VaultProposal{}, errorsmod.Wrapf(types.ErrTooManyPending, "%d proposals pending", general)
	}

	seq, err := k.ProposalSeq.Next(ctx)
	if err != nil {
		return types.VaultProposal{}, err
	}
	id := seq + 1
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	p := types.VaultProposal{
		ProposalId:    id,
		VaultId:       vaultID,
		Proposer:      proposer,
		Messages:      anys,
		MessagesHash:  types.MessagesHash(anys),
		ConfigVersion: v.ConfigVersion,
		Approvals:     []string{proposer},
		Status:        types.ProposalStatus_PROPOSAL_STATUS_PENDING,
		ExpiresAt:     now + v.ProposalTtl,
		Memo:          memo,
	}
	if err := k.VaultProposal.Set(ctx, id, p); err != nil {
		return types.VaultProposal{}, err
	}
	if err := k.PendingByVault.Set(ctx, collections.Join(vaultID, id)); err != nil {
		return types.VaultProposal{}, err
	}
	if err := k.PendingByProposer.Set(ctx, collections.Join3(vaultID, proposer, id)); err != nil {
		return types.VaultProposal{}, err
	}
	if err := k.ExpiryQueue.Set(ctx, collections.Join(p.ExpiresAt, id)); err != nil {
		return types.VaultProposal{}, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_proposal_created",
		sdk.NewAttribute("vault_id", fmt.Sprintf("%d", vaultID)),
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("proposer", proposer),
		sdk.NewAttribute("messages_hash", fmt.Sprintf("%x", p.MessagesHash)),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", p.ExpiresAt)),
	))
	return p, nil
}

// pendingProposal loads a PENDING proposal and its vault, refusing a stale
// config version or a passed expiry even before the EndBlocker gets to it.
func (k Keeper) pendingProposal(ctx context.Context, id uint64) (types.Vault, types.VaultProposal, error) {
	p, err := k.VaultProposal.Get(ctx, id)
	if err != nil {
		return types.Vault{}, types.VaultProposal{}, errorsmod.Wrapf(types.ErrProposalNotFound, "%d", id)
	}
	if p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING {
		return types.Vault{}, p, errorsmod.Wrapf(types.ErrProposalNotPending, "%d is %s", id, p.Status)
	}
	v, err := k.Vault.Get(ctx, p.VaultId)
	if err != nil {
		return types.Vault{}, p, errorsmod.Wrapf(types.ErrVaultNotFound, "%d", p.VaultId)
	}
	if p.ConfigVersion != v.ConfigVersion {
		return v, p, types.ErrConfigChanged
	}
	if sdk.UnwrapSDKContext(ctx).BlockTime().Unix() >= p.ExpiresAt {
		return v, p, types.ErrProposalExpired
	}
	return v, p, nil
}

// Approve records signer's approval on a PENDING proposal.
func (k Keeper) Approve(ctx context.Context, signer string, id uint64, messagesHash []byte) (types.Vault, types.VaultProposal, error) {
	v, p, err := k.pendingProposal(ctx, id)
	if err != nil {
		return v, p, err
	}
	stored, _ := k.signerOf(ctx, v, signer)
	if stored == "" {
		return v, p, types.ErrNotSigner
	}
	if string(messagesHash) != string(p.MessagesHash) {
		return v, p, types.ErrMessagesHashMismatch
	}
	canon := k.canonical(ctx, stored)
	for _, a := range p.Approvals {
		if a == canon || a == stored {
			return v, p, types.ErrAlreadyApproved
		}
	}
	p.Approvals = append(p.Approvals, canon)
	if err := k.VaultProposal.Set(ctx, id, p); err != nil {
		return v, p, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_proposal_approved",
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("approver", canon),
		sdk.NewAttribute("approved_weight", fmt.Sprintf("%d", k.Tally(ctx, v, p))),
	))
	return v, p, nil
}

// RevokeApproval withdraws signer's approval from a PENDING proposal.
func (k Keeper) RevokeApproval(ctx context.Context, signer string, id uint64) error {
	v, p, err := k.pendingProposal(ctx, id)
	if err != nil {
		return err
	}
	stored, _ := k.signerOf(ctx, v, signer)
	if stored == "" {
		return types.ErrNotSigner
	}
	canon := k.canonical(ctx, stored)
	kept := p.Approvals[:0]
	found := false
	for _, a := range p.Approvals {
		if a == canon || a == stored {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return types.ErrNotApproved
	}
	p.Approvals = kept
	if err := k.VaultProposal.Set(ctx, id, p); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_approval_revoked",
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("approver", canon),
	))
	return nil
}

// Execute runs a PENDING proposal whose tally meets the threshold. Inner
// messages run through the router in a CacheContext, all or nothing; the
// proposal is marked EXECUTED before they run so an inner MsgUpdateVault
// never invalidates the proposal carrying it.
func (k Keeper) Execute(ctx context.Context, executor string, id uint64) error {
	v, p, err := k.pendingProposal(ctx, id)
	if err != nil {
		return err
	}
	if stored, _ := k.signerOf(ctx, v, executor); stored == "" {
		return types.ErrNotSigner
	}
	if got := k.Tally(ctx, v, p); got < v.Threshold {
		return errorsmod.Wrapf(types.ErrThresholdNotMet, "approved %d, threshold %d", got, v.Threshold)
	}
	msgs, err := k.checkInnerMsgs(ctx, v, p.Messages)
	if err != nil {
		return err
	}
	if k.late.router == nil {
		return fmt.Errorf("vault: message router not wired")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	if err := k.resolveProposal(cacheCtx, p, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED); err != nil {
		return err
	}
	for i, m := range msgs {
		handler := k.late.router.Handler(m)
		if handler == nil {
			return errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "message %d: no handler for %s", i, sdk.MsgTypeURL(m))
		}
		res, err := handler(cacheCtx, m)
		if err != nil {
			return errorsmod.Wrapf(err, "message %d (%s) failed", i, sdk.MsgTypeURL(m))
		}
		if res != nil {
			cacheCtx.EventManager().EmitEvents(res.GetEvents())
		}
	}
	write()
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("vault_proposal_executed",
		sdk.NewAttribute("vault_id", fmt.Sprintf("%d", p.VaultId)),
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("executor", executor),
	))
	return nil
}

// Cancel marks a PENDING proposal CANCELLED. signer is the proposer (alias
// aware) or the vault address itself (inner message of an executed proposal).
func (k Keeper) Cancel(ctx context.Context, signer string, id uint64) error {
	p, err := k.VaultProposal.Get(ctx, id)
	if err != nil {
		return errorsmod.Wrapf(types.ErrProposalNotFound, "%d", id)
	}
	if p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING {
		return errorsmod.Wrapf(types.ErrProposalNotPending, "%d is %s", id, p.Status)
	}
	v, err := k.Vault.Get(ctx, p.VaultId)
	if err != nil {
		return errorsmod.Wrapf(types.ErrVaultNotFound, "%d", p.VaultId)
	}
	if signer != v.Address && !k.canActAs(ctx, signer, p.Proposer) {
		return types.ErrNotProposer
	}
	if err := k.resolveProposal(ctx, p, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_proposal_cancelled",
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("by", signer),
	))
	return nil
}

// UpdateVault replaces the configuration (signer = vault address, so only via
// an executed proposal), bumps config_version and invalidates every pending
// proposal of the vault. Entries carried over from an accepted entry of the
// previous config stay accepted; new entries start unaccepted.
func (k Keeper) UpdateVault(ctx context.Context, vaultAddr string, signers []types.VaultSigner, threshold uint64, ttl int64, label string) error {
	id, err := k.VaultByAddress.Get(ctx, vaultAddr)
	if err != nil {
		return errorsmod.Wrapf(types.ErrVaultNotFound, "%s", vaultAddr)
	}
	v, err := k.Vault.Get(ctx, id)
	if err != nil {
		return errorsmod.Wrapf(types.ErrVaultNotFound, "%d", id)
	}
	signers = k.withAcceptance(ctx, signers, "", v.Signers)
	if err := k.validateSigners(ctx, id, signers, threshold, ttl, label); err != nil {
		return err
	}
	if err := k.unindexSigners(ctx, id, v.Signers); err != nil {
		return err
	}
	if err := k.indexSigners(ctx, id, signers); err != nil {
		return err
	}
	v.Signers = signers
	v.Threshold = threshold
	v.ProposalTtl = ttl
	v.Label = label
	v.ConfigVersion++
	if err := k.Vault.Set(ctx, id, v); err != nil {
		return err
	}
	if err := k.invalidatePending(ctx, id); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_updated",
		sdk.NewAttribute("vault_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("config_version", fmt.Sprintf("%d", v.ConfigVersion)),
		sdk.NewAttribute("threshold", fmt.Sprintf("%d", threshold)),
		sdk.NewAttribute("signers", fmt.Sprintf("%d", len(signers))),
	))
	return nil
}

// AcceptSigner accepts signer's unaccepted entry in vault id. The entry is
// found through the alias contract (a member who migrated accepts the entry
// listed under the old address with the new key; a tombstoned signer never
// passes). Refused when another accepted entry of the vault resolves to the
// same member (one person would count as two signers) or when the member
// already signs MaxVaultsPerSigner vaults. Does not bump config_version:
// the signer set, weights and threshold are unchanged, the entry only
// starts counting once its owner approves.
func (k Keeper) AcceptSigner(ctx context.Context, signer string, id uint64) error {
	v, err := k.Vault.Get(ctx, id)
	if err != nil {
		return errorsmod.Wrapf(types.ErrVaultNotFound, "%d", id)
	}
	idx := -1
	accepted := false
	for i, s := range v.Signers {
		if !k.canActAs(ctx, signer, s.Address) {
			continue
		}
		if s.Accepted {
			accepted = true
			continue
		}
		if idx < 0 {
			idx = i
		}
	}
	if idx < 0 {
		if accepted {
			return types.ErrAlreadyAccepted
		}
		return types.ErrNotSigner
	}
	entry := v.Signers[idx]
	canon := k.canonical(ctx, entry.Address)
	for i, s := range v.Signers {
		if i != idx && s.Accepted && k.canonical(ctx, s.Address) == canon {
			return errorsmod.Wrapf(types.ErrInvalidSignerSet, "%s and %s are the same member", s.Address, entry.Address)
		}
	}
	n, err := k.countVaultsOfSigner(ctx, entry.Address, id)
	if err != nil {
		return err
	}
	if n >= types.MaxVaultsPerSigner {
		return errorsmod.Wrapf(types.ErrTooManyVaultsForSigner, "%s already signs %d vaults", entry.Address, n)
	}
	if err := k.VaultInvitesBySigner.Remove(ctx, collections.Join(entry.Address, id)); err != nil {
		return err
	}
	v.Signers[idx].Accepted = true
	if err := k.VaultsBySigner.Set(ctx, collections.Join(entry.Address, id)); err != nil {
		return err
	}
	if err := k.Vault.Set(ctx, id, v); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_signer_accepted",
		sdk.NewAttribute("vault_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("signer", entry.Address),
		sdk.NewAttribute("accepted_weight", fmt.Sprintf("%d", k.AcceptedWeight(ctx, v.Signers))),
		sdk.NewAttribute("threshold", fmt.Sprintf("%d", v.Threshold)),
	))
	return nil
}

// LeaveVault removes every entry of vault id that signer can act as (alias
// aware; after a merge one member may hold two). Declining an unaccepted
// listing is the same call. Refused when nothing would remain, when the
// remaining weight (accepted or not) falls below the threshold (the vault
// could never execute again), or when an accepted entry leaves an
// operational vault (accepted weight >= threshold) non-operational. The
// remedy for a refused leave is a MsgUpdateVault by the remaining signers
// first. Removing an accepted entry bumps config_version and invalidates
// the vault's pending proposals, like any change of the operating signer
// set; removing only unaccepted entries does not (they never had weight).
func (k Keeper) LeaveVault(ctx context.Context, signer string, id uint64) error {
	v, err := k.Vault.Get(ctx, id)
	if err != nil {
		return errorsmod.Wrapf(types.ErrVaultNotFound, "%d", id)
	}
	var kept, removed []types.VaultSigner
	removedAccepted := false
	for _, s := range v.Signers {
		if k.canActAs(ctx, signer, s.Address) {
			removed = append(removed, s)
			removedAccepted = removedAccepted || s.Accepted
			continue
		}
		kept = append(kept, s)
	}
	if len(removed) == 0 {
		return types.ErrNotSigner
	}
	if len(kept) == 0 {
		return errorsmod.Wrap(types.ErrLeaveBelowThreshold, "the last signer cannot leave")
	}
	var total uint64
	for _, s := range kept {
		total += s.Weight
	}
	if total < v.Threshold {
		return errorsmod.Wrapf(types.ErrLeaveBelowThreshold, "remaining weight %d, threshold %d", total, v.Threshold)
	}
	if removedAccepted {
		before := k.AcceptedWeight(ctx, v.Signers)
		after := k.AcceptedWeight(ctx, kept)
		if before >= v.Threshold && after < v.Threshold {
			return errorsmod.Wrapf(types.ErrLeaveBelowThreshold, "accepted weight would drop to %d, threshold %d", after, v.Threshold)
		}
	}
	if err := k.unindexSigners(ctx, id, removed); err != nil {
		return err
	}
	v.Signers = kept
	if removedAccepted {
		v.ConfigVersion++
	}
	if err := k.Vault.Set(ctx, id, v); err != nil {
		return err
	}
	if removedAccepted {
		if err := k.invalidatePending(ctx, id); err != nil {
			return err
		}
	}
	for _, s := range removed {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_signer_left",
			sdk.NewAttribute("vault_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("signer", s.Address),
			sdk.NewAttribute("accepted", fmt.Sprintf("%t", s.Accepted)),
			sdk.NewAttribute("config_version", fmt.Sprintf("%d", v.ConfigVersion)),
		))
	}
	return nil
}

// invalidatePending marks every PENDING proposal of the vault INVALIDATED.
// Bounded by MaxSigners × (MaxPendingProposalsPerProposer +
// MaxPendingUpdateProposalsPerProposer).
func (k Keeper) invalidatePending(ctx context.Context, vaultID uint64) error {
	var ids []uint64
	rng := collections.NewPrefixedPairRange[uint64, uint64](vaultID)
	if err := k.PendingByVault.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
		ids = append(ids, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, id := range ids {
		p, err := k.VaultProposal.Get(ctx, id)
		if err != nil {
			if err := k.PendingByVault.Remove(ctx, collections.Join(vaultID, id)); err != nil {
				return err
			}
			continue
		}
		if err := k.resolveProposal(ctx, p, types.ProposalStatus_PROPOSAL_STATUS_INVALIDATED); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("vault_proposal_invalidated",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		))
	}
	return nil
}

// resolveProposal moves a PENDING proposal to a terminal status: drops it
// from the pending indexes and the expiry queue, stamps resolved_at and
// queues it for retention pruning.
func (k Keeper) resolveProposal(ctx context.Context, p types.VaultProposal, status types.ProposalStatus) error {
	if err := k.PendingByVault.Remove(ctx, collections.Join(p.VaultId, p.ProposalId)); err != nil {
		return err
	}
	if err := k.PendingByProposer.Remove(ctx, collections.Join3(p.VaultId, p.Proposer, p.ProposalId)); err != nil {
		return err
	}
	if err := k.ExpiryQueue.Remove(ctx, collections.Join(p.ExpiresAt, p.ProposalId)); err != nil {
		return err
	}
	p.Status = status
	p.ResolvedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.VaultProposal.Set(ctx, p.ProposalId, p); err != nil {
		return err
	}
	return k.RetentionQueue.Set(ctx, collections.Join(p.ResolvedAt, p.ProposalId))
}

// indexProposal (re)builds the index entries of a proposal from its record;
// used by InitGenesis.
func (k Keeper) indexProposal(ctx context.Context, p types.VaultProposal) error {
	if p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING {
		if err := k.PendingByVault.Set(ctx, collections.Join(p.VaultId, p.ProposalId)); err != nil {
			return err
		}
		if err := k.PendingByProposer.Set(ctx, collections.Join3(p.VaultId, p.Proposer, p.ProposalId)); err != nil {
			return err
		}
		return k.ExpiryQueue.Set(ctx, collections.Join(p.ExpiresAt, p.ProposalId))
	}
	return k.RetentionQueue.Set(ctx, collections.Join(p.ResolvedAt, p.ProposalId))
}

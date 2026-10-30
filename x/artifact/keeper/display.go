package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// Query-time display (docs/x-artifact-spec.md §7.17). Stored state never
// changes here; the view picks what clients show.

// tokenView builds the client view of a token. withRenderer runs the class
// renderer (single-token queries only); list queries show stored state.
func (k Keeper) tokenView(ctx context.Context, c types.Class, t types.Token, withRenderer bool) types.TokenView {
	hash := types.MetadataHash(t.Metadata)
	view := types.TokenView{Token: t, MetadataHash: hash, ClassStatus: c.Status, Expired: types.IsExpired(t, now(ctx))}
	if t.Lock == types.TokenLock_TOKEN_LOCK_RENTED {
		// A rental past its term (not yet ended by the EndBlocker) no
		// longer names a user: services gating on user must not keep
		// admitting the renter (§6).
		if r, err := k.Rentals.Get(ctx, collections.Join(t.ClassId, t.Id)); err == nil && r.ExpiresAt > now(ctx) {
			view.User = r.User
		}
	}
	if t.Harberger != nil {
		price, _ := types.CurrentHarbergerPrice(c.Harberger, *t.Harberger, now(ctx))
		view.HarbergerPrice = price.String()
	}
	if t.Descendants > 0 {
		view.ContentsHash, _ = k.ContentsHash(ctx, t)
	}
	if k.treeWithheld(ctx, t) || c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN || t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		view.Token.Metadata = types.TokenMetadata{}
		view.Token.Schedule = nil
		view.Token.MediaVersions = nil
		view.Withheld = true
		return view
	}
	if c.Reveal.Kind == types.RevealKind_REVEAL_KIND_GENERATIVE_SEED && c.Reveal.RevealedAt != 0 {
		if seed, err := hex.DecodeString(c.Reveal.Seed); err == nil {
			view.Seed = types.TokenSeed(seed, t.ClassId, t.Id)
		}
	}
	view.DisplayedMetadata, view.DisplaySource = types.StoredDisplay(t, now(ctx))
	// The holder's explicit media choice outranks the renderer; an
	// unrevealed token has nothing to render yet.
	if !withRenderer || !c.Renderer.HasRenderer() || view.DisplaySource == types.DisplayMediaVersion || t.Unrevealed {
		return view
	}
	meta, err := k.render(ctx, c, t, view.Seed, view.User)
	if err != nil {
		view.RenderError = err.Error()
		return view
	}
	view.DisplayedMetadata, view.DisplaySource = meta, types.DisplayRenderer
	return view
}

// render evaluates the class renderer for a token and validates its output
// like stored metadata. Any failure (gnovm unwired, renderers disabled,
// VM panic or out-of-gas, bad JSON, metadata rule) is returned so the
// caller falls back to stored metadata.
func (k Keeper) render(ctx context.Context, c types.Class, t types.Token, seed, user string) (types.TokenMetadata, error) {
	var meta types.TokenMetadata
	p := k.GetParams(ctx)
	if !p.RenderersEnabled {
		return meta, errorsmod.Wrap(types.ErrRendererUnavailable, "renderers are disabled")
	}
	if k.late.gnovm == nil {
		return meta, types.ErrRendererUnavailable
	}
	expr := types.RenderExpr(c.Id, t.Id, seed, now(ctx), height(ctx))
	if c.Renderer.StateVersion != 0 {
		expr = types.RenderStateExpr(c.Id, t.Id, seed, now(ctx), height(ctx), k.renderState(ctx, c, t, user))
	}
	out, err := k.evalRenderer(ctx, c.Renderer.PkgPath, expr)
	if err != nil {
		return meta, fmt.Errorf("renderer eval: %w", err)
	}
	if len(out) > types.MaxRenderOutputBytes {
		return meta, fmt.Errorf("renderer output exceeds %d bytes", types.MaxRenderOutputBytes)
	}
	if err := k.cdc.UnmarshalJSON([]byte(out), &meta); err != nil {
		return types.TokenMetadata{}, fmt.Errorf("renderer output is not TokenMetadata JSON: %w", err)
	}
	if err := types.ValidateTokenMetadata(meta, p); err != nil {
		return types.TokenMetadata{}, fmt.Errorf("renderer output: %w", err)
	}
	return meta, nil
}

// evalRenderer runs the VM and turns a panic into an error.
func (k Keeper) evalRenderer(ctx context.Context, pkgPath, expr string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("renderer panic: %v", r)
		}
	}()
	out, err = k.late.gnovm.EvalString(ctx, pkgPath, expr)
	return out, err
}

// renderState builds the state document of the class's renderer version:
// v1 is the token's own fields, its renter and its owner's x/rep standing;
// v2 adds the current season, the owner's season standing and the token's
// nested contents (direct children and the contents hash).
func (k Keeper) renderState(ctx context.Context, c types.Class, t types.Token, user string) string {
	member, level := false, int32(0)
	if owner, err := k.addressCodec.StringToBytes(t.Owner); err == nil && k.isActiveMember(ctx, owner) {
		member = true
		if lvl, err := k.late.rep.GetTrustLevel(ctx, owner); err == nil {
			level = int32(lvl)
		}
	}
	v1 := types.NewRenderStateV1(c, t, user, member, level)
	if c.Renderer.StateVersion == types.RenderStateV1 {
		return v1.JSON()
	}
	var season types.SeasonInfo
	var standing types.MemberSeason
	if k.late.season != nil {
		if s, err := k.late.season.CurrentSeason(ctx); err == nil {
			season = s
		}
		if m, found, err := k.late.season.MemberSeason(ctx, t.Owner); err == nil && found {
			standing = m
		}
	}
	var children []string
	if kids, err := k.directChildren(ctx, t.ClassId, t.Id); err == nil {
		for _, r := range kids {
			children = append(children, refString(r))
		}
	}
	hash, getErr := k.ContentsHash(ctx, t)
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("ContentsHash failed", "err", getErr)
	}
	return types.NewRenderStateV2(v1, season, standing, children, hash).JSON()
}

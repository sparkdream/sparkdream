package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

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
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN || t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
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
	meta, err := k.render(ctx, c, t, view.Seed)
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
func (k Keeper) render(ctx context.Context, c types.Class, t types.Token, seed string) (types.TokenMetadata, error) {
	var meta types.TokenMetadata
	p := k.GetParams(ctx)
	if !p.RenderersEnabled {
		return meta, errorsmod.Wrap(types.ErrRendererUnavailable, "renderers are disabled")
	}
	if k.late.gnovm == nil {
		return meta, types.ErrRendererUnavailable
	}
	out, err := k.evalRenderer(ctx, c.Renderer.PkgPath, types.RenderExpr(c.Id, t.Id, seed, now(ctx), height(ctx)))
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

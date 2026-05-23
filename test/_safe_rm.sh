# ============================================================================
# Safe directory-tree removal helper (sourced by other test scripts).
#
# Per docs/development-conventions.md: `rm -rf` is forbidden project-wide.
# An unguarded `rm -rf "$X"` with an empty or unexpected `$X` can escalate
# from a no-op to wiping the user's home directory. We use `find -delete`
# instead, with explicit guards on the path so a misconfigured variable
# cannot escalate.
#
# Public surface:
#   safe_rmtree <path>          — remove path's contents and the path itself
#   safe_rmtree_contents <path> — remove path's contents but keep the directory
#
# Both functions:
#   - Return 0 if the path does not exist (idempotent — no error to clean
#     something that's already gone).
#   - Refuse to operate on the empty string, "/", "$HOME", or any path that
#     does not start with "/" (relative paths slip too easily through
#     misexpansion).
#   - Use `find -depth -delete` so files are removed before their parent
#     directories, matching the semantics of `rm -rf` on a directory tree.
#
# Source this once per script that needs the helpers (idempotent guard).
# ============================================================================

# Idempotent source guard
if [ "${_SAFE_RM_LOADED:-0}" = "1" ]; then
    return 0 2>/dev/null || true
fi
_SAFE_RM_LOADED=1

# Internal: validate that a path is safe to feed into `find -delete`.
_safe_rm_validate() {
    local p="$1"
    local label="${2:-safe_rmtree}"
    if [ -z "$p" ]; then
        echo "$label: refusing to delete empty path" >&2
        return 1
    fi
    case "$p" in
        /) echo "$label: refusing to delete /" >&2 ; return 1 ;;
        /*) ;;
        *) echo "$label: refusing relative path: $p" >&2 ; return 1 ;;
    esac
    if [ -n "${HOME:-}" ] && [ "$p" = "$HOME" ]; then
        echo "$label: refusing to delete \$HOME ($p)" >&2
        return 1
    fi
    return 0
}

# Remove a path (file, directory, or symlink) and everything under it.
# Returns 0 if the path is missing or successfully removed.
safe_rmtree() {
    local p="$1"
    _safe_rm_validate "$p" safe_rmtree || return 1
    [ -e "$p" ] || [ -L "$p" ] || return 0
    find "$p" -depth -delete 2>/dev/null
    # Surface a clear error if the find left any residue (e.g. permission
    # denied on a single file under the tree).
    if [ -e "$p" ] || [ -L "$p" ]; then
        echo "safe_rmtree: failed to fully remove $p" >&2
        return 1
    fi
    return 0
}

# Remove the contents of a directory but leave the directory itself in place.
# Useful for cleaning chain data dirs (LevelDBs etc.) without disturbing
# permissions on the parent.
safe_rmtree_contents() {
    local p="$1"
    _safe_rm_validate "$p" safe_rmtree_contents || return 1
    [ -d "$p" ] || return 0
    # -mindepth 1 keeps the parent directory itself.
    find "$p" -mindepth 1 -depth -delete 2>/dev/null
    return 0
}

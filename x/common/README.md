# `x/common`

The `x/common` package provides shared type definitions and utilities used across multiple modules. It is not a standalone Cosmos SDK module — it contains no keeper, no genesis, and no message server.

> **Note:** `Tag`, `ReservedTag`, and the `TagKeeper` interface live in `x/rep`. See [`docs/x-rep-spec.md`](../../docs/x-rep-spec.md) (Tag Registry section). Tag-format validation helpers live in `x/common` since they are pure utilities with no storage dependency.

## Overview

This package provides:

- **Content types** — standardized `ContentType` enum for post body format interpretation (text, HTML, markdown, compressed, off-chain references)
- **Moderation vocabulary** — standardized `ModerationReason` enum and `FlagRecord` struct used by content modules
- **Tag validation helpers** — pure format/length validators (`ValidateTagFormat`, `ValidateTagLength`) reused by every module that accepts tag input

## Types

### ContentType

Tells the frontend how to interpret post/reply body content. Used by `x/blog` and `x/forum`.

| Range | Type | Description |
|-------|------|-------------|
| 0 | `UNSPECIFIED` | Default/unknown |
| 1-9 | On-chain text | Human-readable strings |
| 1 | `TEXT` | Plain text |
| 2 | `HTML` | HTML markup |
| 3 | `MARKDOWN` | Markdown |
| 10-19 | On-chain compressed | Base64-encoded binary strings |
| 10 | `GZIP` | Gzip-compressed |
| 11 | `ZSTD` | Zstandard-compressed |
| 20+ | Off-chain references | URI/hash strings |
| 20 | `IPFS` | IPFS CID |
| 21 | `ARWEAVE` | Arweave transaction ID |
| 22 | `FILECOIN` | Filecoin CID |
| 23 | `JACKAL` | Jackal Protocol reference |

### FlagRecord

```protobuf
message FlagRecord {
  string            flagger     = 1;  // who flagged
  ModerationReason  reason      = 2;  // standardized reason
  string            reason_text = 3;  // custom explanation
  int64             flagged_at  = 4;  // timestamp
  cosmos.math.Int   weight      = 5;  // flagger's voting power
}
```

### ModerationReason

| Value | Description |
|-------|-------------|
| `UNSPECIFIED` | Default/unknown |
| `SPAM` | Spam or unsolicited content |
| `HARASSMENT` | Harassment or bullying |
| `MISINFORMATION` | Misleading information |
| `OFF_TOPIC` | Not relevant to context |
| `LOW_QUALITY` | Below quality standards |
| `INAPPROPRIATE` | Inappropriate content |
| `IMPERSONATION` | Impersonating another user |
| `POLICY_VIOLATION` | Violates platform policies |
| `DUPLICATE` | Duplicate content |
| `SCAM` | Scam or fraud |
| `COPYRIGHT` | Copyright violation |
| `OTHER` | Other reason (see reason_text) |

## Helpers

### Tag Validation

Two helper functions provide consistent tag-string checking across all modules. They are pure functions (no ctx/state), so every module is free to call them without importing a keeper.

```go
// ValidateTagFormat checks if a tag name matches the required pattern.
// Pattern: ^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$ — lowercase alphanumeric
// with optional hyphens, no leading/trailing hyphens. Single-character tags
// are allowed.
func ValidateTagFormat(name string) bool

// ValidateTagLength checks if a tag name is within the maximum length.
func ValidateTagLength(name string, maxLen uint64) bool
```

## Consumers

| Module | Uses |
|--------|------|
| `x/blog` | `ContentType` for post/reply body format |
| `x/forum` | `ContentType`, `ModerationReason`, `FlagRecord`, tag-validation helpers |
| `x/collect` | `ModerationReason`, `FlagRecord` for content flagging |
| `x/rep` | Tag-validation helpers (for `MsgCreateTag`, initiative/reputation tag validation); `Tag`/`ReservedTag` storage now lives natively in `x/rep` |

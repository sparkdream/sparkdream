# `x/common`

The `x/common` package provides shared type definitions and interfaces used across multiple modules. It is not a standalone Cosmos SDK module — it contains no keeper, no genesis, and no message server.

## Overview

This package provides:

- **Content types** — standardized `ContentType` enum for post body format interpretation (text, HTML, markdown, compressed, off-chain references)
- **Tag system** — consistent tag format validation and the `TagKeeper` interface for cross-module tag operations
- **Moderation vocabulary** — standardized `ModerationReason` enum and `FlagRecord` struct used by content modules
- **Reserved tags** — governance-controlled tag reservation with member-use permissions

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

### Tag

```protobuf
message Tag {
  string name           = 1;  // lowercase alphanumeric + hyphens
  uint64 usage_count    = 2;  // total uses across modules
  int64  created_at     = 3;  // creation timestamp
  int64  last_used_at   = 4;  // last use timestamp
  int64  expiration_index = 5; // for cleanup tracking
}
```

**Validation pattern**: `^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$` — lowercase alphanumeric with optional hyphens, no leading/trailing hyphens. Single-character tags are allowed.

### ReservedTag

```protobuf
message ReservedTag {
  string name            = 1;  // reserved tag name
  string authority        = 2;  // who reserved it (council address)
  bool   members_can_use  = 3;  // whether members can apply this tag
}
```

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

## Interfaces

### TagKeeper

The `TagKeeper` interface is implemented by `x/forum` (which owns tag storage) and consumed by `x/rep`, `x/collect`, and `x/blog` for tag validation and usage tracking.

```go
type TagKeeper interface {
    TagExists(ctx context.Context, name string) (bool, error)
    IsReservedTag(ctx context.Context, name string) (bool, error)
    GetTag(ctx context.Context, name string) (Tag, error)
    IncrementTagUsage(ctx context.Context, name string, timestamp int64) error
}
```

### Tag Validation

Two helper functions provide consistent tag checking across all modules:

```go
// ValidateTagFormat checks if a tag name matches the required pattern.
func ValidateTagFormat(name string) bool

// ValidateTagLength checks if a tag name is within the maximum length.
func ValidateTagLength(name string, maxLen uint64) bool
```

## Consumers

| Module | Uses |
|--------|------|
| `x/blog` | Uses `ContentType` for post/reply body format |
| `x/forum` | Implements `TagKeeper`; uses `ContentType`, `ModerationReason`, `FlagRecord` |
| `x/collect` | Uses `ModerationReason`, `FlagRecord` for content flagging |
| `x/rep` | Uses `TagKeeper` for initiative/reputation tag validation |

# `x/common`

The `x/common` package provides shared type definitions and interfaces used across multiple modules. It is not a standalone Cosmos SDK module — it contains no keeper, no genesis, and no message server.

## Overview

This package provides:

- **Tag system** — consistent tag format validation and the `TagKeeper` interface for cross-module tag operations
- **Moderation vocabulary** — standardized `ModerationReason` enum and `FlagRecord` struct used by content modules
- **Reserved tags** — governance-controlled tag reservation with member-use permissions

## Types

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
    IncrementTagUsage(ctx context.Context, name string) error
    DecrementTagUsage(ctx context.Context, name string) error
    GetTag(ctx context.Context, name string) (*Tag, error)
}
```

### Tag Validation

The `ValidateTagFormat` function provides consistent tag format checking across all modules:

```go
func ValidateTagFormat(name string, maxLength int) error
```

## Consumers

| Module | Uses |
|--------|------|
| `x/forum` | Implements `TagKeeper`; uses `ModerationReason`, `FlagRecord` |
| `x/blog` | Uses `TagKeeper` for tag validation |
| `x/collect` | Uses `ModerationReason`, `FlagRecord` for content flagging |
| `x/rep` | Uses `TagKeeper` for initiative/reputation tag validation |

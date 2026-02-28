# Technical Specification: `x/name`

## 1. Abstract

The `x/name` module implements a governance-controlled name registration and identity system for the Spark Dream appchain. It provides human-readable identity mapping (e.g., "alice" → `sprkdrm1x...`), reverse name resolution, and dispute resolution mechanisms.

Key principles:
- **Council-gated registration**: Only Commons Council members can register names ("The Republic" model)
- **Name scavenging**: Inactive owners lose names after expiration period
- **Fee-based disputes**: Formal challenge mechanism with council arbitration

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/commons` | Source of Commons Council group ID and policy address |
| `x/group` | Membership verification for council members |
| `x/bank` | Fee collection for registration and disputes |
| `x/auth` | Address codec for bech32 conversion |

---

## 3. State Objects

### 3.1. NameRecord

Primary mapping from name to owner.

```protobuf
message NameRecord {
  string name = 1;   // Registered name (lowercase, validated)
  string owner = 2;  // Owner's bech32 address
  string data = 3;   // Arbitrary metadata (IPFS hash, profile JSON, etc.)
}
```

### 3.2. OwnerInfo

Owner metadata for reverse lookups and expiration tracking.

```protobuf
message OwnerInfo {
  string address = 1;          // Owner's address (bech32)
  string primary_name = 2;     // Primary name for reverse resolution
  int64 last_active_time = 3;  // Unix timestamp of last activity
}
```

### 3.3. Dispute

Active dispute record proving fee payment.

```protobuf
message Dispute {
  string name = 1;      // Name being disputed
  string claimant = 2;  // Address claiming ownership
}
```

### 3.4. Params

```protobuf
message Params {
  repeated string blocked_names = 1;          // Reserved names (admin, founder, etc.)
  uint64 min_name_length = 2;                 // Minimum characters (default: 3)
  uint64 max_name_length = 3;                 // Maximum characters (default: 30)
  uint64 max_names_per_address = 4;           // Names per owner limit (default: 5)
  google.protobuf.Duration expiration_duration = 5;  // Inactivity timeout (default: 1 year)
  cosmos.base.v1beta1.Coin registration_fee = 6;     // Fee to register (default: 10 SPARK)
  cosmos.base.v1beta1.Coin dispute_fee = 7;          // Fee to challenge (default: 500 SPARK)
}
```

---

## 4. Storage Schema

Using Cosmos SDK collections framework:

| Collection | Key | Value | Purpose |
|------------|-----|-------|---------|
| `Names` | name (string) | NameRecord | Primary lookup: name → owner + data |
| `Owners` | address (string) | OwnerInfo | Reverse lookup + activity tracking |
| `Disputes` | name (string) | Dispute | Active dispute tracking |
| `OwnerNames` | (address, name) pair | - | Secondary index for "names by owner" queries |

---

## 5. Messages

### 5.1. RegisterName

Register a new name or scavenge an expired name.

```protobuf
message MsgRegisterName {
  string registrant = 1;  // Must be Commons Council member
  string name = 2;        // Name to register
  string data = 3;        // Optional metadata
}
```

**Validation:**
- Name format: lowercase alphanumeric + hyphens (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
- Length: `min_name_length` ≤ len ≤ `max_name_length`
- Not in `blocked_names` list
- Registrant is Commons Council member

**Logic:**
1. Validate name format and length
2. Check council membership via `x/commons` + `x/group`
3. Deduct `registration_fee` from registrant
4. If name exists:
   - If owner active → reject with `ErrNameTaken`
   - If owner expired → scavenge (transfer ownership)
5. Enforce `max_names_per_address` limit
6. Store NameRecord, update OwnerInfo
7. Auto-set as primary if first name for owner
8. Emit `name_registered` or `name_scavenged` event

### 5.2. SetPrimary

Set the primary name for reverse resolution.

```protobuf
message MsgSetPrimary {
  string owner = 1;  // Must own the name
  string name = 2;   // Name to set as primary
}
```

**Logic:**
1. Verify sender owns the name
2. Update `OwnerInfo.PrimaryName`

### 5.3. UpdateName

Update metadata for an owned name.

```protobuf
message MsgUpdateName {
  string owner = 1;  // Must own the name
  string name = 2;   // Name to update
  string data = 3;   // New metadata
}
```

**Logic:**
1. Verify sender owns the name
2. Update `NameRecord.Data`
3. Emit `name_updated` event

### 5.4. FileDispute

Challenge name ownership (any address can file).

```protobuf
message MsgFileDispute {
  string claimant = 1;  // Address claiming the name
  string name = 2;      // Name being disputed
}
```

**Logic:**
1. Verify name exists
2. Transfer `dispute_fee` to Commons Council policy address
3. Create Dispute record (proof of fee payment)

### 5.5. ResolveDispute

Council resolves a dispute (governance action).

```protobuf
message MsgResolveDispute {
  string authority = 1;   // Must be Commons Council policy address
  string name = 2;        // Disputed name
  string new_owner = 3;   // Address to receive the name
}
```

**Logic:**
1. Verify sender is Commons Council policy address
2. Verify dispute exists (proves fee was paid)
3. Transfer ownership:
   - Remove from old owner's index
   - Add to new owner's index
   - Update NameRecord.Owner
4. Delete dispute record

### 5.6. UpdateParams

Governance parameter update.

```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be x/gov module account
  Params params = 2;     // New parameters
}
```

---

## 6. Queries

| Query | Input | Output | Description |
|-------|-------|--------|-------------|
| `Resolve` | name | NameRecord | Name → address + data lookup |
| `ReverseResolve` | address | string | Address → primary name lookup |
| `Names` | address, pagination | []NameRecord | List all names owned by address |
| `GetDispute` | name | Dispute | Get active dispute for name |
| `ListDispute` | pagination | []Dispute | List all active disputes |
| `Params` | - | Params | Get current module parameters |

---

## 7. Business Logic

### 7.1. Council Membership Check

```go
func (k Keeper) IsCommonsCouncilMember(ctx sdk.Context, memberAddr sdk.AccAddress) bool {
    // Get Commons Council group ID from x/commons
    council, found := k.commonsKeeper.GetExtendedGroup(ctx, "Commons Council")
    if !found {
        return false
    }

    // Check membership via x/group
    groups := k.groupKeeper.GroupsByMember(ctx, memberAddr)
    for _, group := range groups {
        if group.GroupId == council.GroupId {
            return true
        }
    }
    return false
}
```

### 7.2. Name Scavenging

Names become available for scavenging when owner is inactive beyond `expiration_duration`:

```go
func (k Keeper) IsOwnerExpired(ctx sdk.Context, ownerAddr sdk.AccAddress) bool {
    lastActive := k.GetLastActiveTime(ctx, ownerAddr)
    expiryTime := time.Unix(lastActive, 0).Add(k.GetParams(ctx).ExpirationDuration)
    return ctx.BlockTime().After(expiryTime)
}
```

When registering an existing name:
- If owner is expired → name is scavenged (transferred to new registrant)
- If owner is active → registration rejected

### 7.3. Activity Tracking

`last_active_time` is updated on:
- Name registration
- Name update
- Setting primary name

This creates a simple inactivity-based expiration without oracles.

---

## 8. Default Parameters

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| `blocked_names` | 100+ reserved | Prevent impersonation (admin, founder, etc.) |
| `min_name_length` | 3 | Prevent single-char squatting |
| `max_name_length` | 30 | Reasonable display length |
| `max_names_per_address` | 5 | Prevent hoarding |
| `expiration_duration` | 1 year | Balance between ownership security and cleanup |
| `registration_fee` | 10 SPARK | Spam prevention |
| `dispute_fee` | 500 SPARK | Prevent frivolous challenges |

---

## 9. Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidSigner` | 1100 | Non-governance signer for UpdateParams |
| `ErrNameTaken` | 1101 | Name already registered with active owner |
| `ErrNameNotFound` | 1102 | Name doesn't exist |
| `ErrInvalidName` | 1103 | Failed format validation |
| `ErrNameReserved` | 1104 | Name in blocked list |
| `ErrTooManyNames` | 1105 | Owner hit max names limit |
| `ErrDisputeNotFound` | 1106 | No active dispute for name |

---

## 10. Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `name_registered` | name, owner | New name registration |
| `name_scavenged` | name, old_owner, new_owner | Expired name transferred |
| `name_updated` | name, owner, data | Metadata changed |
| `dispute_filed` | name, claimant | New dispute created |
| `dispute_resolved` | name, old_owner, new_owner | Council resolved dispute |

---

## 11. Integration Points

### 11.1. x/commons Integration

```go
// Get Commons Council for membership check
council, _ := k.commonsKeeper.GetExtendedGroup(ctx, "Commons Council")

// Get council policy address for dispute fees
policyAddr := k.GetCouncilAddress(ctx, council.GroupId)
```

### 11.2. x/group Integration

```go
// Check if address is member of a group
groups := k.groupKeeper.GroupsByMember(ctx, memberAddr)
```

### 11.3. x/bank Integration

```go
// Collect registration fee
k.bankKeeper.SendCoinsFromAccountToModule(ctx, registrant, "name", fee)

// Send dispute fee to council
k.bankKeeper.SendCoins(ctx, claimant, councilPolicyAddr, disputeFee)
```

---

## 12. Security Considerations

### 12.1. Council-Only Registration

Registration is restricted to Commons Council members to prevent:
- Name squatting by speculators
- Impersonation attacks
- Namespace pollution

### 12.2. Blocked Names

A comprehensive list of reserved names prevents:
- `admin`, `administrator`, `root`, `system` - System impersonation
- `founder`, `council`, `committee` - Governance impersonation
- Brand names and trademarks

### 12.3. Dispute Resolution

The dispute mechanism:
- Requires significant fee (500 SPARK) to prevent spam
- Council arbitration ensures human judgment
- Fee goes to council treasury (not burned)

### 12.4. Scavenging Attack Prevention

- Only council members can register (limits attackers)
- 1-year expiration gives owners ample time to renew activity
- Scavenging emits events for monitoring

---

## 13. Future Considerations

1. **Name Transfers**: Allow direct owner-to-owner transfers without disputes
2. **Subdomain Support**: Hierarchical names (e.g., `project.alice`)
3. **Name Auctions**: Auction mechanism for premium names
4. **Integration with x/rep**: Tie name privileges to reputation level
5. **ENS Compatibility**: Cross-chain name resolution

---

## 14. File References

- Proto definitions: `proto/sparkdream/name/v1/*.proto`
- Keeper logic: `x/name/keeper/`
- Types and errors: `x/name/types/`
- Module setup: `x/name/module/`
- Integration tests: `test/name/`

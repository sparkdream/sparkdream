# Technical Specification: `x/name`

## 1. Abstract

The `x/name` module implements a governance-controlled name registration and identity system for the Spark Dream appchain. It provides human-readable identity mapping (e.g., "alice" → `sprkdrm1x...`), reverse name resolution, and dispute resolution mechanisms.

Key principles:
- **Council-gated registration**: Only Commons Council members can register names ("The Republic" model)
- **Name scavenging**: Inactive owners lose names after expiration period
- **DREAM-staked disputes**: Formal challenge mechanism with DREAM staking, owner contest option, and jury/council arbitration
- **BeginBlocker auto-resolution**: Uncontested disputes auto-resolve after timeout

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/commons` | Council membership verification, policy address, Operations Committee authorization |
| `x/bank` | Fee collection for registration |
| `x/auth` | Address codec for bech32 conversion |
| `x/rep` | DREAM token operations (Lock/Unlock/Burn) for dispute staking via `dreamutil.Ops` |

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

Active dispute record with DREAM staking and contest tracking.

```protobuf
message Dispute {
  string name = 1;                    // Name being disputed
  string claimant = 2;               // Address claiming ownership
  int64 filed_at = 3;                // Block height when filed
  string stake_amount = 4;           // Claimant's DREAM stake (cosmos.Int)
  bool active = 5;                   // Whether dispute is pending
  string contest_challenge_id = 6;   // Set when owner contests (for jury tracking)
  int64 contested_at = 7;            // Block height when contested
  bool contest_succeeded = 8;        // Jury verdict: did owner's contest win?
}
```

### 3.4. DisputeStake

Tracks the claimant's DREAM stake for a name dispute.

```protobuf
message DisputeStake {
  string challenge_id = 1;  // "name_dispute:<name>:<block>"
  string staker = 2;        // Claimant address
  string amount = 3;        // DREAM stake amount (cosmos.Int)
}
```

### 3.5. ContestStake

Tracks the owner's DREAM stake when contesting a dispute.

```protobuf
message ContestStake {
  string challenge_id = 1;  // "name_contest:<name>:<block>"
  string owner = 2;         // Current owner address
  string amount = 3;        // DREAM stake amount (cosmos.Int)
}
```

### 3.6. Params

```protobuf
message Params {
  repeated string blocked_names = 1;                 // Reserved names (admin, founder, etc.)
  uint64 min_name_length = 2;                        // Minimum characters (default: 3)
  uint64 max_name_length = 3;                        // Maximum characters (default: 30)
  uint64 max_names_per_address = 4;                  // Names per owner limit (default: 5)
  google.protobuf.Duration expiration_duration = 5;  // Inactivity timeout (default: 1 year)
  cosmos.base.v1beta1.Coin registration_fee = 6;     // Fee to register (default: 10 SPARK)
  string dispute_stake_dream = 7;                    // DREAM staked by claimant (cosmos.Int, default: 50)
  uint64 dispute_timeout_blocks = 8;                 // Blocks before uncontested dispute auto-resolves (default: 100800 ~7 days)
  string contest_stake_dream = 9;                    // DREAM staked by owner when contesting (cosmos.Int, default: 100)
}
```

### 3.7. NameOperationalParams

Subset of `Params` updateable by the Commons Council Operations Committee without a full governance proposal. Governance-only fields (`blocked_names`, `min_name_length`, `max_name_length`, `max_names_per_address`) are excluded.

```protobuf
message NameOperationalParams {
  google.protobuf.Duration expiration_duration = 1;
  cosmos.base.v1beta1.Coin registration_fee = 2;
  string dispute_stake_dream = 3;      // cosmos.Int
  uint64 dispute_timeout_blocks = 4;
  string contest_stake_dream = 5;      // cosmos.Int
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
| `DisputeStakes` | challenge_id (string) | DisputeStake | Claimant DREAM stake tracking |
| `ContestStakes` | challenge_id (string) | ContestStake | Owner contest DREAM stake tracking |

---

## 5. Messages

### 5.1. RegisterName

Register a new name or scavenge an expired name.

```protobuf
message MsgRegisterName {
  string authority = 1;  // Must be Commons Council member
  string name = 2;       // Name to register
  string data = 3;       // Optional metadata
}
```

**Validation:**
- Name format: lowercase alphanumeric + hyphens (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
- Length: `min_name_length` ≤ len ≤ `max_name_length`
- Not in `blocked_names` list
- Registrant is Commons Council member

**Logic:**
1. Validate name format and length
2. Check council membership via `x/commons` (`HasMember`)
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
  string authority = 1;  // Must own the name
  string name = 2;       // Name to set as primary
}
```

**Logic:**
1. Verify sender owns the name
2. Update `OwnerInfo.PrimaryName`

### 5.3. UpdateName

Update metadata for an owned name.

```protobuf
message MsgUpdateName {
  string creator = 1;  // Must own the name
  string name = 2;     // Name to update
  string data = 3;     // New metadata
}
```

**Logic:**
1. Verify sender owns the name
2. Update `NameRecord.Data`
3. Emit `name_updated` event

### 5.4. FileDispute

Challenge name ownership with DREAM staking (any address can file).

```protobuf
message MsgFileDispute {
  string authority = 1;  // Claimant address
  string name = 2;       // Name being disputed
  string reason = 3;     // Why the claimant believes they should own this name
}
```

**Logic:**
1. Verify name exists and has an owner
2. Check no active dispute already exists for this name
3. Lock claimant's DREAM stake (`dispute_stake_dream` from params) via `dreamOps.Lock()`
4. Create Dispute record with `filed_at` block height, `active = true`
5. Store DisputeStake record with challenge ID `"name_dispute:<name>:<block>"`
6. Emit `name_dispute_filed` event

### 5.5. ContestDispute

Allows the current name owner to contest a filed dispute, triggering jury review.

```protobuf
message MsgContestDispute {
  string authority = 1;  // Current owner address
  string name = 2;       // Name with active dispute
  string reason = 3;     // Why the dispute is invalid
}
```

**Logic:**
1. Verify active dispute exists for the name
2. Verify dispute has not already been contested (`contest_challenge_id` must be empty)
3. Verify sender is the current name owner
4. Verify contest period hasn't expired (current block <= `filed_at + dispute_timeout_blocks`)
5. Lock owner's DREAM stake (`contest_stake_dream` from params) via `dreamOps.Lock()`
6. Generate contest challenge ID `"name_contest:<name>:<block>"` and update dispute record (`contest_challenge_id`, `contested_at`)
7. Store ContestStake record
8. Emit `name_dispute_contested` event (jury integration via x/rep happens off-chain or via event listeners)

### 5.6. ResolveDispute

Resolves a dispute after jury verdict or governance decision.

```protobuf
message MsgResolveDispute {
  string authority = 1;       // Must be governance authority, Council policy, or Operations Committee policy
  string name = 2;            // Disputed name
  string new_owner = 3;       // Only used if transfer_approved
  bool transfer_approved = 4; // true = name transfers to claimant, false = dismiss dispute
}
```

**Authorization:** governance authority, Commons Council policy address, or Operations Committee policy address.

**Logic:**
1. Verify sender is authorized (governance, Council, or Operations Committee)
2. Verify active dispute exists
3. Resolve based on whether the dispute was contested:
   - **Contested path** (`contest_challenge_id` is set):
     - If `transfer_approved`: transfer name to claimant, unlock claimant stake, burn owner's contest stake
     - If dismissed: name stays, unlock owner's contest stake, burn claimant stake
     - Clean up ContestStake record
   - **Uncontested path** (`contest_challenge_id` is empty):
     - If `transfer_approved`: transfer name to claimant, unlock claimant stake
     - If dismissed: name stays, burn claimant stake
4. Clean up DisputeStake record
5. Set dispute `active = false`
6. Emit `name_dispute_resolved` event with outcome (`upheld` or `dismissed`) and contested flag

### 5.7. UpdateParams

Governance parameter update.

```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be x/gov module account
  Params params = 2;     // New parameters
}
```

### 5.8. UpdateOperationalParams

Operational parameter update by Commons Council Operations Committee (no full governance proposal required).

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;                          // Operations Committee member or governance authority
  NameOperationalParams operational_params = 2;  // Operational parameters to update
}
```

**Logic:**
1. Verify sender is authorized via `isCouncilAuthorized(ctx, addr, "commons", "operations")`
2. Validate the operational params
3. Merge with current params (governance-only fields preserved, operational fields overwritten)
4. Validate merged params
5. Store merged params

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
func (k Keeper) IsCommonsCouncilMember(ctx context.Context, memberAddr string) (bool, error) {
    // Check membership via x/commons HasMember
    // "Commons Council" is the group index name
    return k.commonsKeeper.HasMember(ctx, "Commons Council", memberAddr)
}
```

### 7.2. Name Scavenging

Names become available for scavenging when owner is inactive beyond `expiration_duration`:

```go
func (k Keeper) IsOwnerExpired(ctx sdk.Context, ownerAddr sdk.AccAddress) bool {
    lastActive := k.GetLastActiveTime(ctx, ownerAddr)
    if lastActive == 0 {
        return false  // Never active = assume active (safety)
    }
    expiryTime := time.Unix(lastActive, 0).Add(params.ExpirationDuration)
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

### 7.4. BeginBlocker: Auto-Resolution of Expired Disputes

The BeginBlocker iterates all active disputes each block and auto-resolves uncontested disputes that have exceeded `dispute_timeout_blocks`:

```go
func (k Keeper) BeginBlocker(ctx context.Context) error {
    return k.processExpiredDisputes(ctx)
}
```

**Auto-resolution rules:**
- Only **uncontested** disputes (no `contest_challenge_id`) are auto-resolved
- Contested disputes wait for explicit jury verdict via `MsgResolveDispute`
- Uncontested timeout = dispute **upheld**: name transfers to claimant, claimant's DREAM stake is returned
- Emits `name_dispute_expired_upheld` event
- Errors are logged but do not halt the chain

### 7.5. Dispute Resolution Flow

Two resolution paths exist:

**Path 1: Uncontested (owner does not respond)**
1. Claimant files dispute → DREAM locked
2. `dispute_timeout_blocks` pass with no contest
3. BeginBlocker auto-resolves: name transfers, claimant stake returned

**Path 2: Contested (owner responds)**
1. Claimant files dispute → claimant DREAM locked
2. Owner contests within timeout → owner DREAM locked, jury review triggered
3. Jury/council issues verdict via `MsgResolveDispute`
4. Winner's stake returned, loser's stake burned

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
| `dispute_stake_dream` | 50 DREAM | Skin-in-the-game for challengers (aligned with x/season report stake) |
| `dispute_timeout_blocks` | 100800 (~7 days) | Time for owner to contest before auto-resolution (aligned with x/season appeal period) |
| `contest_stake_dream` | 100 DREAM | Higher stake for owner to show conviction (aligned with x/season appeal stake) |

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
| `ErrDisputeAlreadyExists` | 1107 | Active dispute already exists for this name |
| `ErrDisputeNotActive` | 1108 | Dispute is not active |
| `ErrNotNameOwner` | 1109 | Sender is not the name owner |
| `ErrContestAlreadyFiled` | 1110 | Dispute has already been contested |
| `ErrContestPeriodExpired` | 1111 | Contest period has expired |
| `ErrDREAMOperationFailed` | 1112 | DREAM token operation failed (lock/unlock/burn) |
| `ErrNotAuthorized` | 1113 | Sender not authorized for this action |

---

## 10. Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `name_registered` | name, owner | New name registration |
| `name_scavenged` | name, old_owner, new_owner | Expired name transferred |
| `name_updated` | name, owner, new_data | Metadata changed |
| `name_dispute_filed` | name, claimant, stake_amount, reason, filed_at | New dispute created with DREAM stake |
| `name_dispute_contested` | name, owner, contest_stake, contest_challenge_id, reason, contested_at | Owner contests a dispute |
| `name_dispute_resolved` | name, claimant, outcome (upheld/dismissed), contested | Dispute resolved by authority or jury |
| `name_dispute_expired_upheld` | name, claimant, expired_at_block | Uncontested dispute auto-resolved by BeginBlocker |

---

## 11. Integration Points

### 11.1. x/commons Integration

```go
// Check council membership (replaces old x/group approach)
isMember, _ := k.commonsKeeper.HasMember(ctx, "Commons Council", memberAddr.String())

// Check Operations Committee authorization for operational param updates
authorized := k.commonsKeeper.IsCouncilAuthorized(ctx, addr, "commons", "operations")

// Get council policy address for dispute resolution authorization
group, _ := k.commonsKeeper.GetGroup(ctx, "Commons Council")
policyAddr := group.PolicyAddress
```

### 11.2. x/bank Integration

```go
// Collect registration fee
k.bankKeeper.SendCoinsFromAccountToModule(ctx, registrant, "name", fee)
```

### 11.3. x/rep Integration (DREAM Token Operations)

DREAM operations are performed through the `dreamutil.Ops` abstraction, which delegates to the `RepKeeper` interface:

```go
// Lock DREAM tokens when filing a dispute
k.dreamOps.Lock(ctx, claimantAddr, stakeAmount)

// Lock DREAM tokens when contesting a dispute
k.dreamOps.Lock(ctx, ownerAddr, contestStakeAmount)

// Unlock DREAM tokens for the winning party
k.dreamOps.Unlock(ctx, winnerAddr, stakeAmount)

// Burn DREAM tokens of the losing party
k.dreamOps.Burn(ctx, loserAddr, stakeAmount)

// Settle stakes in contested disputes (unlock winner, burn loser)
k.dreamOps.SettleStakes(ctx, winnerAddr, winnerAmount, loserAddr, loserAmount)
```

**RepKeeper interface:**
```go
type RepKeeper interface {
    LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
    UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
    BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
}
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

The dispute mechanism uses DREAM staking with two resolution paths:

**Uncontested path:** If the owner does not contest within `dispute_timeout_blocks`, the BeginBlocker auto-resolves the dispute in favor of the claimant (name transfers, claimant's DREAM stake is returned).

**Contested path:** If the owner contests, both parties have DREAM at stake. A jury verdict (via x/rep) or council decision determines the outcome. The loser's DREAM stake is burned; the winner's is returned.

- Requires 50 DREAM stake to file (skin-in-the-game, not just fees)
- Owner must stake 100 DREAM to contest (higher conviction required)
- Losing party's stake is burned (prevents frivolous challenges and bad-faith contests)
- Resolution authorized for governance, Commons Council, or Operations Committee

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

# x/season Integration Tests

This directory contains end-to-end (e2e) integration bash test scripts for the `x/season` module.

## Prerequisites

1. **Chain Running**: The sparkdream chain must be running
   ```bash
   cd /home/chill/cosmos/sparkdream/sparkdream
   ignite chain serve
   ```

2. **Genesis Accounts**: The chain must be initialized with genesis accounts (alice, bob, etc.)

3. **x/rep Module**: Since x/season depends on x/rep for membership, accounts must first be registered in x/rep

## Test Files

| File | Description |
|------|-------------|
| `setup_test_accounts.sh` | Creates and funds test accounts, invites them to x/rep |
| `profile_test.sh` | Tests display names, usernames, display titles, achievements |
| `guild_test.sh` | Tests guild creation, joining, leaving, officers, invites |
| `guild_advanced_test.sh` | Tests kick, transfer founder, dissolve, claim founder |
| `quest_test.sh` | Tests quest listing, starting, progress, abandoning, completing |
| `season_test.sh` | Tests season queries, parameters, transitions, extensions |
| `display_name_moderation_test.sh` | Tests display name reporting and appeals |
| `xp_tracking_test.sh` | Tests XP trackers, vote records, forum cooldowns, title eligibility |
| `run_all_tests.sh` | Master test runner for all tests |

## Running Tests

### Run Full Test Suite

```bash
cd test/season
bash run_all_tests.sh
```

### Run Individual Tests

```bash
# Setup first (creates test accounts and invites them to x/rep)
bash setup_test_accounts.sh

# Then run individual tests
bash profile_test.sh
bash guild_test.sh
bash guild_advanced_test.sh
bash quest_test.sh
bash season_test.sh
bash display_name_moderation_test.sh
bash xp_tracking_test.sh
```

### Command Line Options

The master test runner supports options to skip specific tests:

```bash
bash run_all_tests.sh --help

Options:
  --no-setup          Skip setup_test_accounts.sh
  --no-profile        Skip profile_test.sh
  --no-guild          Skip guild_test.sh
  --no-guild-advanced Skip guild_advanced_test.sh
  --no-quest          Skip quest_test.sh
  --no-season         Skip season_test.sh
  --no-moderation     Skip display_name_moderation_test.sh
  --no-xp-tracking    Skip xp_tracking_test.sh
  --only-setup        Run only setup (skip all tests)
```

## Test Accounts

The setup script creates these test accounts:

| Account | Purpose |
|---------|---------|
| `guild_founder` | Guild creation and management |
| `guild_officer` | Guild officer operations |
| `guild_member1` | Guild member operations |
| `guild_member2` | Guild member operations |
| `quest_user` | Quest operations |
| `display_user` | Display name/profile operations |

All accounts are:
- Funded with SPARK (for gas fees)
- Invited to x/rep as members
- Funded with DREAM (for guild creation costs, stakes, etc.)

## Environment File

After setup, a `.test_env` file is created with all test account addresses:

```bash
source .test_env
echo $GUILD_FOUNDER_ADDR
echo $GUILD_OFFICER_ADDR
# etc.
```

## Test Coverage

### Profile Tests
- Get member profile
- Set display name
- Display name validation
- Set username
- Query by display name
- Query titles and achievements

### Guild Tests
- List guilds
- Create guild
- Query guild details
- Join guild (public)
- Set invite-only
- Invite to guild
- Accept invite
- Promote to officer
- Query guild members
- Update description
- Demote officer
- Leave guild
- Query by founder

### Guild Advanced Tests
- Create test guild for advanced operations
- Add members to guild
- Kick member from guild (officer)
- Transfer guild founder
- Dissolve guild
- Claim guild founder (frozen guild)
- Query guilds by founder (including dissolved)
- Query guild membership details
- List all guild memberships

### Quest Tests
- List quests
- Create quest (requires authority)
- Query available quests
- Query quest by ID
- Start quest
- Query quest status
- Abandon quest
- Query quest chain
- Deactivate quest (requires authority)
- Claim quest reward

### Season Tests
- Query current season
- Query season by number
- Query season stats
- Query module parameters
- Query next season info
- Set next season info (requires authority)
- Query transition state
- Extend season (requires authority)
- Query recovery state
- Query season snapshots
- Query member season history
- Query member XP history
- Admin transition controls (requires authority)

### Display Name Moderation Tests
- Set display name
- Query moderations
- Report display name
- Query report stakes
- Appeal moderation
- Query appeal stakes
- Moderation parameters

### XP Tracking Tests
- List epoch XP trackers
- Get specific XP tracker
- List vote XP records
- Get specific vote XP record
- List forum XP cooldowns
- Get specific forum cooldown
- List member registrations
- Get specific registration
- List season title eligibility
- Get specific title eligibility
- Query transition recovery state
- Query member XP history
- List all titles
- Query member titles

## Authority-Gated Operations

Some operations require special authority (Commons Council or Operations Committee):

- `create-quest`
- `deactivate-quest`
- `set-next-season-info`
- `extend-season`
- `abort-season-transition`
- `retry-season-transition`
- `skip-transition-phase`

These tests will attempt the operations but expect them to fail without proper authority.

## Troubleshooting

### "Chain is not running"
Start the chain with `ignite chain serve`

### "Alice account not found"
The chain needs to be initialized with genesis accounts

### "not a member" errors
Run `setup_test_accounts.sh` first to invite accounts to x/rep

### "insufficient funds" errors
Accounts may need more DREAM. Run setup again or manually transfer DREAM.

# X/Forum Module E2E Integration Tests

This directory contains end-to-end integration tests for the `x/forum` module using the `sparkdreamd` CLI.

## Prerequisites

1. **Running Chain**: The sparkdreamd chain must be running locally
2. **Alice Account**: An account named "alice" with sufficient SPARK and DREAM tokens
3. **x/rep Module**: The rep module must be functional for membership-related features

## Quick Start

```bash
# Navigate to test directory
cd test/forum

# Run all tests
./run_all_tests.sh

# Or run with specific exclusions
./run_all_tests.sh --no-setup  # Skip account setup if already done
```

## Test Files

| File | Description |
|------|-------------|
| `setup_test_accounts.sh` | Creates and funds test accounts, sets up initial state |
| `category_test.sh` | Tests category creation, querying, and permissions |
| `post_test.sh` | Tests post/thread creation, editing, voting, following |
| `sentinel_test.sh` | Tests sentinel bonding, flagging, hiding, locking |
| `bounty_test.sh` | Tests bounty creation, assignment, and awards |
| `moderation_test.sh` | Tests thread moving, pinning, member reports |
| `tag_budget_test.sh` | Tests tag budget creation, top-up, awards, withdrawals |
| `appeals_test.sh` | Tests post/thread appeals, hide records, lock records |
| `advanced_test.sh` | Tests freeze, archive, tag reports, proposed replies |
| `run_all_tests.sh` | Master test runner with CLI options |

## Test Accounts

The setup script creates the following test accounts:

| Account | Purpose |
|---------|---------|
| `poster1` | Primary poster for thread/reply tests |
| `poster2` | Secondary poster for multi-user scenarios |
| `sentinel1` | Primary sentinel for moderation tests |
| `sentinel2` | Secondary sentinel |
| `bounty_creator` | Creates and manages bounties |
| `moderator` | Administrative moderation tests |

## Running Individual Tests

```bash
# Run only post tests
./post_test.sh

# Run only sentinel tests
./sentinel_test.sh

# Run only bounty tests
./bounty_test.sh
```

## Test Runner Options

```bash
./run_all_tests.sh [OPTIONS]

Options:
  --no-setup        Skip account setup (use if already done)
  --no-post         Skip post/thread tests
  --no-category     Skip category tests
  --no-sentinel     Skip sentinel tests
  --no-bounty       Skip bounty tests
  --no-moderation   Skip moderation tests
  --no-tag-budget   Skip tag budget tests
  --no-appeals      Skip appeals tests
  --no-advanced     Skip advanced tests
  --help, -h        Show help message
```

## Test Coverage

### Category Tests (`category_test.sh`)
- List existing categories
- Create new category (authority required)
- Query category details
- Create members-only category
- Create admin-only category
- Verify category counts

### Post Tests (`post_test.sh`)
- Create thread (root post)
- Query post by ID
- Create reply to thread
- Edit post content
- Delete post
- Upvote/downvote posts
- Follow/unfollow thread
- Mark accepted reply
- Query thread replies
- Query posts by author

### Sentinel Tests (`sentinel_test.sh`)
- Bond as sentinel
- Query sentinel status
- Flag post for review
- Hide flagged post
- Lock thread
- Unlock thread
- Dismiss flags
- Query sentinel activity
- Unbond sentinel
- Query active sentinels

### Bounty Tests (`bounty_test.sh`)
- Create bounty
- Query bounty details
- Increase bounty amount
- Assign bounty to reply
- Award bounty
- Cancel bounty
- Query active bounties
- Query expiring bounties

### Moderation Tests (`moderation_test.sh`)
- Move thread to different category
- Appeal thread move
- Pin post
- Unpin post
- Pin reply
- Unpin reply
- Dispute pin
- Report member
- Cosign member report
- Defend against report
- Query member standing
- Query flag review queue

### Tag Budget Tests (`tag_budget_test.sh`)
- Create tag budget
- Query tag budget
- List all tag budgets
- Top up tag budget
- Toggle tag budget (deactivate/reactivate)
- Award from tag budget
- Withdraw from tag budget
- Query budgets by creator
- Query active budgets

### Appeals Tests (`appeals_test.sh`)
- Setup sentinel and posts
- Hide post (setup for appeal)
- Query hide record
- Appeal post
- Query appeal cooldown
- Lock thread (setup for lock appeal)
- Query thread lock record
- Appeal thread lock
- Query locked threads
- Query thread lock status
- List all hide records
- List all lock records
- Query gov action appeals
- Appeal gov action
- Query sentinel bond commitment

### Advanced Tests (`advanced_test.sh`)
- Freeze thread (time-lock)
- Query archive cooldown
- Query archived threads
- Unarchive thread
- Query archive metadata
- Report tag
- Query tag reports
- Resolve tag report (authority required)
- Resolve member report (authority required)
- Query member warnings
- Query member salvation status
- Set forum paused (authority required)
- Query forum status
- Set moderation paused (authority required)
- Confirm/reject proposed reply
- Unpin reply
- Query jury participation

## Environment File

After running setup, a `.test_env` file is created containing test account addresses:

```bash
POSTER1_ADDR=sprkdrm1...
POSTER2_ADDR=sprkdrm1...
SENTINEL1_ADDR=sprkdrm1...
SENTINEL2_ADDR=sprkdrm1...
BOUNTY_CREATOR_ADDR=sprkdrm1...
MODERATOR_ADDR=sprkdrm1...
TEST_CATEGORY_ID=1
```

## Notes

- Some operations require operations committee membership and may fail gracefully
- Sentinel operations require DREAM tokens for bonding
- Bounty operations require sufficient DREAM balance
- Tests use 6-second sleep for transaction confirmation
- All tests output detailed progress and results

## Troubleshooting

### "Chain is not running"
Start the chain: `sparkdreamd start`

### "Alice account not found"
Create the account: `sparkdreamd keys add alice --keyring-backend test`

### "Insufficient DREAM balance"
Ensure Alice has DREAM tokens or adjust the funding amounts in setup

### "Transaction failed"
Check the error message for specific issues. Common causes:
- Insufficient fees
- Account not a member (for membership-required operations)
- Authority required for governance operations

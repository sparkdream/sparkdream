# Governance (x/gov) Integration Tests

This directory contains integration tests for the Cosmos SDK `x/gov` module and its interaction with Spark Dream's custom governance structure.

## Tests

### `inflation_immutable_test.sh`

**Purpose:** Verify that inflation parameters are immutable via governance proposals.

**What it tests:**
1. ✅ Current inflation parameters match expected values (2% - 5%)
2. ✅ Mint module authority is set to burn address
3. ✅ Governance proposal to change inflation params FAILS at submission
4. ✅ If proposal is created, it FAILS at execution
5. ✅ Inflation parameters remain unchanged after attack attempt

**Security guarantee:**
- `inflation_min`, `inflation_max`, `goal_bonded`, and `mint_denom` cannot be changed via `x/gov` proposals
- Only coordinated chain upgrades can modify these values
- Protects against malicious governance attacks on monetary policy

**How it works:**
The mint module authority is set to a burn address (`sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe`) that has no private key. This makes it cryptographically impossible to sign `MsgUpdateParams` messages, causing all parameter update attempts to fail.

**Expected output:**
```
✅ TEST PASSED: Inflation parameters are IMMUTABLE via governance!
```

**Run the test:**
```bash
# From project root
./test/gov/inflation_immutable_test.sh

# Or run all tests
./test/run_all_tests.sh
```

## Test Requirements

- Ignite CLI or sparkdreamd binary available
- Test keyring with `alice` and `bob` accounts
- Chain running locally with test configuration
- `jq` installed for JSON parsing

## Related Documentation

- [docs/security-hardening.md](../../docs/security-hardening.md) - Security architecture
- [docs/tokenomics.md](../../docs/tokenomics.md) - Token economics and inflation model
- [app/app_config.go](../../app/app_config.go) - Mint module authority configuration

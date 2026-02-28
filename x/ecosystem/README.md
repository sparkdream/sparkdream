# `x/ecosystem`

The `x/ecosystem` module is a minimal governance-controlled treasury that enables `x/gov` to spend from the ecosystem module account for chain-wide initiatives.

## Overview

This module provides:

- **Governance-gated spending** — only `x/gov` authority can transfer funds from the ecosystem account
- **Independent treasury** — separate from `x/split` distribution logic; holds funds for ecosystem-wide decisions

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSpend` | Transfer coins from ecosystem account to a recipient | `x/gov` authority only |
| `MsgUpdateParams` | Update module parameters | `x/gov` authority only |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters (currently empty) |

## Parameters

None defined. The module params exist as a placeholder for future governance-controlled configuration.

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Module account access |
| `x/bank` | Yes | Coin transfers from module account |
| `x/staking` | Yes | Validator consensus address codec |

## Client

### CLI

```bash
# Spend from ecosystem treasury (via governance proposal)
sparkdreamd tx ecosystem spend [recipient] [amount] --from authority

# Query
sparkdreamd q ecosystem params
```

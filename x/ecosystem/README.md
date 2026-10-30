# `x/ecosystem`

The `x/ecosystem` module is a minimal governance-controlled treasury that enables `x/gov` to spend from the ecosystem module account for chain-wide initiatives.

## Overview

This module provides:

- **Governance-gated spending** — only `x/gov` authority can transfer funds from the ecosystem account
- **Independent treasury** — separate from `x/split` distribution logic; holds funds for ecosystem-wide decisions

### Funding

Nothing funds the module account automatically: no genesis allocation, no
x/split share, and `MsgCommunityPoolSpend` is hard-rejected by x/guardian.
It receives funds only through direct bank sends to the module address
(anyone may donate). It is not the "Ecosystem pillar" of the revenue split:
that 20% flows through x/split to the Ecosystem Council's policy address,
which spends it through council proposals. Routing community-pool funds here
would need an x/split share for the module address, which no message can
create today (shares follow x/commons groups). Keeping the module dormant
this way is the decided design: no funding path beyond direct sends.

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSpend` | Transfer coins from ecosystem account to a recipient (both addresses parsed with the module's address codec; emits `ecosystem_spend` with `authority`, `recipient`, `amount`) | `x/gov` authority only |
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
| `x/staking` | Yes | Injected by the Ignite scaffold (depinject) and unused; kept by decision rather than altering the generated pattern |

## Client

### CLI

```bash
# Spend from ecosystem treasury (via governance proposal)
sparkdreamd tx ecosystem spend [recipient] [amount] --from authority

# Query
sparkdreamd q ecosystem params
```

# `x/sparkdream`

The `x/sparkdream` module is the root namespace module for the Spark Dream chain. It serves as a minimal placeholder for chain-level parameters and governance configuration.

## Overview

This module provides:

- **Namespace anchor** — primary module identity for the chain (`ModuleName: "sparkdream"`)
- **Content license** — the chain-wide `ContentLicense` query: everything on Spark Dream is published under CC0 1.0 ([docs/content-license.md](../../docs/content-license.md))
- **Governance-ready params** — `MsgUpdateParams` infrastructure in place for future chain-level configuration
- **Extensibility** — proto/type structure ready for expansion without breaking changes

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update module parameters | `x/gov` authority only |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters (currently empty) |
| `ContentLicense` | The license all content is published under (`CC0-1.0`), its name and legal-text URL, the dedication participants make by submitting content, and the licenses federated content must carry (`CC0-1.0`, `PDM-1.0`). Reads no state: the values are compiled constants from `x/common`. CLI: `sparkdreamd query sparkdream content-license`; REST: `GET /sparkdream/sparkdream/v1/content_license` |

## Parameters

None defined. Proto definitions exist for future extensibility.

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address validation |
| `x/bank` | Yes | Future use |

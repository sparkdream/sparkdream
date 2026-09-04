# sim — devnet activity simulations

Script-driven simulations that make the four devnet accounts (`acct-alice` … `acct-dave`) behave
like a real, active community, so a running devnet has organic on-chain activity. Each simulation
is self-contained, runs unattended for a long time with **no LLM in the loop**, and can optionally
be driven **live by a model** via a Hermes plugin.

## Layout

```
sim/<network>/<name>/        e.g. sim/devnet/rep, sim/devnet/gov
```

- `<network>` — the target chain tier (currently only `devnet`).
- `<name>` — the simulation type. Each is a self-contained package with the standard files below.

Current sims:

| Sim | What it exercises | Guide |
|-----|-------------------|-------|
| [devnet/rep](devnet/rep) | Team/reputation activity: initiatives, forum threads, replies, conviction staking | [../docs/devnet-team-simulation-agent-guide.md](../docs/devnet-team-simulation-agent-guide.md) |
| [devnet/gov](devnet/gov) | Governance: Commons Council proposals (submit → vote → execute), optional x/gov | [../docs/devnet-governance-simulation-agent-guide.md](../docs/devnet-governance-simulation-agent-guide.md) |

## Standard layout (every sim looks the same)

| File | Role |
|------|------|
| `sim_env.sh` | Config (chain-id, node, denom, accounts) + `send_tx` / `wait_id` helpers. Sourced by every script. |
| `sim_init.sh` | One-time setup: writes `state.json`, seeds the content pool. |
| `sim_round.sh` | Runs **one** round from a few text args; prints **one** compact summary line. All tx JSON discarded. |
| `sim_loop.sh` | Drives rounds from the pool on an interval, unattended (no model). |
| `sim_status.sh` | One-line status read. No args. |
| `sim_agent.sh` | Self-contained OpenAI-tools agent loop (live model via llama.cpp), for harnesses without a plugin system. |
| `content_pool.example.jsonl` | Seed rounds so the loop runs before you generate more. |
| `hermes_refill_prompt.txt` | Prompt to paste into Hermes to mass-produce more pool rounds. |
| `hermes_tools.json` | The tools as Hermes/OpenAI function schemas. |
| `hermes_dispatch.sh` | Glue: runs Hermes's `<tool_call>` output, returns a `<tool_response>`. |
| `hermes_plugin/` | Native Hermes plugin (`plugin.yaml` + `__init__.py` + `schemas.py` + `tools.py`). |

Sim-specific extras keep a descriptive name (no `sim_` prefix): e.g. rep's `debug_round.sh`, gov's
`track_b.sh`.

## Per-sim identity (so sims coexist)

The **files are identical** across sims; the **identity** is qualified by the sim `<name>` so
multiple sims can be installed and run at once (distinct Hermes plugins, distinct state):

| Aspect | Convention | rep | gov |
|--------|-----------|-----|-----|
| Data dir | `~/.sparkdream-<name>` | `~/.sparkdream-rep` | `~/.sparkdream-gov` |
| Plugin env var | `SPARKDREAM_<NAME>_DIR` | `SPARKDREAM_REP_DIR` | `SPARKDREAM_GOV_DIR` |
| Plugin name | `sparkdream-<name>` | `sparkdream-rep` | `sparkdream-gov` |
| Tool names | `<name>_round` / `_status` / `_selftest` | `rep_round`, … | `gov_round`, … |

## Design principles (shared by all sims)

- **Quiet tools.** A whole round is one `sim_round.sh` call that returns **one line**; all `tx`/`query`
  JSON stays in the shell. A small-context model can drive hundreds of rounds without compacting.
- **State in a file.** Cross-round state (whose turn, queues, counters) lives in
  `~/.sparkdream-<name>/state.json`, never in a model's context — so any script works in a fresh
  shell and the loop can stop/resume anytime.
- **Model optional.** `sim_loop.sh` runs unattended from a pre-generated pool; the Hermes plugin /
  `sim_agent.sh` put a live model in the loop one round per turn. Same scripts either way.
- **Honest failures.** `send_tx` returns non-zero when nothing broadcast (missing key, no funds,
  malformed tx), so a failed step can't masquerade as `ok` in the summary line.

## Adding a new sim

1. `cp -r sim/devnet/rep sim/devnet/<name>` (rep is the simplest template).
2. Rewrite `sim_round.sh` for the new activity; keep the "one call, one line, state in a file"
   shape. Adjust `content_pool.example.jsonl` + `hermes_refill_prompt.txt` to the new round fields.
3. Set the per-sim identity: plugin `name: sparkdream-<name>`, env `SPARKDREAM_<NAME>_DIR`, tools
   `<name>_round/<name>_status/<name>_selftest`, data dir `~/.sparkdream-<name>`.
4. Keep the shared gotchas: keyring names default to the `acct-` prefix (`ACCT_PREFIX` in
   `sim_env.sh`); don't let `send_tx` report false success; enum CLI flags take lowercase values
   (e.g. `--content-type markdown`, not `CONTENT_TYPE_MARKDOWN`).

## Running one

See each sim's own `README.md` (e.g. [devnet/rep/README.md](devnet/rep/README.md)) for quick-start,
the Hermes plugin install, and the split VM + llama.cpp-over-headscale setup.

# Development Conventions

Project-wide engineering conventions that are referenced from code comments, test scripts, and other spec docs. Keep this doc authoritative — when a section here is referenced elsewhere by name (e.g. "AppModule Value-Copy Bug"), update the cross-references if you rename a section.

Related references:
- [security-hardening.md](security-hardening.md) — immutable parameters (mint inflation authority, committee constraints, Three Pillars hierarchy).
- [tokenomics.md](tokenomics.md) — SPARK / DREAM economics; the burn-address authority pattern on mint params is referenced by the [x/guardian](../x/guardian/) module.

---

## Build & Run

### Use `ignite chain build` + `ignite chain init` + `sparkdreamd start`, not `ignite chain serve`

`ignite chain serve` runs an internal build pipeline that regenerates proto files and can produce different artifacts than `ignite chain build`. When `ignite chain build` succeeds but `ignite chain serve` panics (e.g. depinject cyclic-dependency errors), the issue is ignite's serve pipeline, not the chain code.

Use this workflow instead:

```bash
ignite chain build
ignite chain init
sparkdreamd start --home ~/.sparkdream
```

If you must use `ignite chain serve`, always pass `-o <logfile>` to disable the bubbletea TUI (which crashes without an interactive terminal):

```bash
ignite chain serve --reset-once -o /tmp/ignite-serve.log &
```

### Stale `sparkdreamd` binary problem

`ignite chain serve` may pick up a stale `sparkdreamd` binary in the project root (`./sparkdreamd`), `build/`, or `/tmp/sparkdreamd` instead of the GOPATH binary. After proto schema changes these local copies become outdated and produce wrong genesis or depinject panics.

**Invariant**: there must only be ONE `sparkdreamd` binary — the one at `$(go env GOPATH)/bin/sparkdreamd` installed by `ignite chain build`.

Cleanup:

```bash
rm -f ./sparkdreamd build/sparkdreamd /tmp/sparkdreamd
rm -f ~/.ignite/local-chains/sparkdream/exported_genesis.json
```

The `exported_genesis.json` cache is also worth deleting — ignite will merge it into a fresh genesis if present.

### Ignite scaffolding & proto generation

- `ignite scaffold` commands ALWAYS take `-y` to auto-answer the uncommitted-changes prompt (the prompt requires a TTY and will hang otherwise). Example: `ignite scaffold message Foo bar:string --module mymodule --signer creator -y`.
- `ignite generate proto-go` likewise takes `-y`. Example: `ignite generate proto-go -y`.
- Use `ignite scaffold` to add new messages, queries, types, or modules. Scaffolding generates correct boilerplate (unique store keys, proper sim weights) and avoids the subtle bugs that manual duplication produces.
- Do NOT alter Ignite-generated patterns (`query_params.go`, `genesis.go`, codec registration) without checking the diff against another module first. These follow standard Cosmos SDK conventions shared across modules.

---

## `rm -rf` is forbidden project-wide

An unguarded `rm -rf "$X"` with an empty or unexpected `$X` can escalate from a no-op to wiping the user's home directory. Project-wide rule: do not use `rm -rf` in scripts.

Use the helpers in [test/_safe_rm.sh](../test/_safe_rm.sh) instead:

- `safe_rmtree <path>` — remove the path's contents and the path itself.
- `safe_rmtree_contents <path>` — remove the path's contents but keep the directory.

Both refuse to operate on `""`, `/`, `$HOME`, or relative paths. They use `find -depth -delete` internally so files are removed before their parent directories, matching `rm -rf` semantics on a directory tree.

For one-off cleanups inside a script that already validates its path, `find <path> -delete` (or `find <path> -mindepth 1 -delete` to keep the parent) is the direct equivalent.

---

## Depinject Cyclic Dependencies

In Cosmos SDK depinject, even `optional:"true"` dependencies participate in cycle detection. The ONLY way to break a keeper-graph cycle is to completely remove the interface from `ModuleInputs` and wire it manually in `app.go` via a `Set*Keeper()` method after `depinject.Inject()` returns.

Many cross-module keepers in this project are wired this way — see the late-wiring section in [app/app.go](../app/app.go) (~ lines 228-259 at the time of writing). When adding new cross-module dependencies, wire them in `app.go` rather than adding optional depinject fields.

The convenience of `optional:"true"` is exactly the trap: it looks like cycle-breaking but is not.

---

## AppModule Value-Copy Bug

**Symptom**: a keeper has its `Set*Keeper()` setter called from `app.go` after depinject returns, but the message server (or invariants, hooks, etc. that go through `AppModule`) still sees `nil` for that dependency.

**Cause**: `AppModule.keeper` is stored as a **value type**, not a pointer. `ProvideModule` constructs the keeper `k`, then calls `NewAppModule(k)` — the resulting `AppModule` holds a **snapshot** of `k`. Post-depinject `Set*Keeper()` calls in `app.go` modify the exported `app.FooKeeper` field but NOT the snapshot inside the `AppModule`. The msg_server reads `am.keeper.dep`, sees the snapshot's `nil`, and silently no-ops (or panics, depending on the code path).

**Fix**: add the late-wired keeper as an `optional:"true"` depinject input on `ModuleInputs`, and wire it onto the keeper inside `ProvideModule` BEFORE the `NewAppModule(*k)` call. That way the value-copy captures the populated dependency.

For more complex graphs (multiple late keepers, cyclic deps that can't be broken by depinject ordering alone), use a `lateKeepers struct` pointer that the keeper holds and that `app.go` mutates after `depinject.Inject()` — both the snapshot and the exported keeper share the pointer, so writes are visible everywhere. See `x/rep` and `x/commons` for the pattern.

Examples of the value-copy fix in the codebase:
- [x/blog/module/depinject.go](../x/blog/module/depinject.go) — `RepKeeper`, `IdentityKeeper` wired before `NewAppModule`.
- [x/name/module/depinject.go](../x/name/module/depinject.go) — `IdentityKeeper`.
- [x/forum/module/depinject.go](../x/forum/module/depinject.go) — `IdentityKeeper`.
- [x/collect/module/depinject.go](../x/collect/module/depinject.go) — `IdentityKeeper`.
- [x/federation/module/depinject.go](../x/federation/module/depinject.go) — same pattern.
- [x/identity/module/depinject.go](../x/identity/module/depinject.go) — keeper held by pointer specifically to dodge this bug for the `bankKeeper` field.

**Identity is the canonical example**: it wires several SDK keepers as `*Keeper`-by-pointer for the same reason — see [x/identity/module/module.go](../x/identity/module/module.go).

---

## Amino Names on Every Signer Msg

Every proto message that has `option (cosmos.msg.v1.signer) = "...";` MUST also have:

```proto
option (amino.name) = "sparkdream/x/<module>/Msg<Name>";
```

…and the file must `import "amino/amino.proto";`.

The SDK's `cosmossdk.io/x/tx/signing/aminojson` handler reads this option to build `SIGN_MODE_LEGACY_AMINO_JSON` sign-docs. **Without it, Ledger users get "signature verification failed"** because Keplr+Ledger only supports amino-JSON signing.

The regression guard at [x/commons/types/amino_name_test.go](../x/commons/types/amino_name_test.go) catches missing/wrong amino names for commons messages — extend it (or add a sibling test) when adding signer Msgs to other modules. (x/collect has a service-walking sibling at [x/collect/types/amino_name_test.go](../x/collect/types/amino_name_test.go) that covers new messages automatically — prefer that pattern.)

Additional rationale worth knowing:

- The other failure symptom besides "signature verification failed" is Ledger rejecting the sign-doc outright with "JSON Dictionaries are not sorted".
- Amino names are part of the signing contract with wallets — never rename one once clients depend on it; the regression test pins the exact strings.
- Client side: `@sparkdreamnft/sparkdreamjs` must be regenerated/republished after amino-name additions so its amino converters emit the same `type` strings the chain produces.
- Messages that nest `repeated google.protobuf.Any` (e.g. `MsgSubmitProposal`, `MsgSubmitAnonymousProposal`) need CUSTOM client-side amino converters: the default `Any` encoder base64-dumps proto bytes, which Ledger's strict validator rejects. Each inner message must be recursively amino-encoded as `{type: <amino.name>, value: <amino-form>}` with alphabetically sorted keys at every nesting level.

---

## Test Script Patterns

- Test scripts live under `test/<module>/`. Each module has `setup_test_accounts.sh`, `run_all_tests.sh`, and individual `*_test.sh` files.
- Snapshots: `test/<module>/snapshots/post-setup/` for fast iteration. `--save-setup` creates a snapshot, `--restore-setup` runs from snapshot.
- The parallel runner is [test/run_parallel.sh](../test/run_parallel.sh).
- Proto3 JSON omits zero-value fields. When parsing query responses with `jq`, use `(.field // 0)` defaults for any field that could legitimately be zero/empty.
- Avoid mocking the proto LegacyDec round-trip — `cosmossdk.io/math.LegacyDec` JSON output is the internal integer representation (e.g. `"1000000000000000000"` for `1.0`); shoving that back into a tx message double-encodes. Convert before re-sending.

---

## When in Doubt

- Module specs live in `docs/x-<module>-spec.md`.
- Architecture overview: [architecture.md](architecture.md).
- Tokenomics: [tokenomics.md](tokenomics.md).
- Security: [security-hardening.md](security-hardening.md), [security-audit.md](security-audit.md), [security-audit-app.md](security-audit-app.md).

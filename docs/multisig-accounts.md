# Multisig Accounts

Spark Dream supports the Cosmos SDK's native k-of-n multisig account
(`LegacyAminoPubKey`) as a first-class account type. It is the right tool for
**one person protecting their own account** with several devices (a Ledger, a
phone, a paper backup). It is not a shared treasury with rotatable signers; that
is x/vault ([x-vault-spec.md](x-vault-spec.md)).

## When to use it

| Need | Use |
|---|---|
| Lose one device without losing the account; no single hot key can drain it | **Native multisig** (this document) |
| Day-to-day posting, voting, reacting without collecting signatures each time | Native multisig as the cold root plus an **x/session session key** as the hot key (below) |
| Several people sharing one treasury, signers that change over time, approvals from Keplr | **x/vault** ([x-vault-spec.md](x-vault-spec.md)): keyless stable address, weighted rotatable signers, approvals by ordinary txs |

## What works today

- **Every message.** The ante chain verifies multisig signatures exactly as the
  SDK does; `tx_sig_limit` is 7 on every network, so the practical ceiling is
  seven sub-keys. x/guardian floors `tx_sig_limit` at 7 so governance cannot
  lock existing multisig accounts out.
- **Ledger co-signers.** Every signer message carries `(amino.name)`, so a
  Ledger sub-key signs amino-JSON for any message. The CLI multisign flow is
  amino-JSON throughout: `tx sign --multisig` and `tx multisign` verify every
  sub-signature under one sign mode.
- **Membership.** A multisig address can be invited and accept the invitation
  as any address would. The chain cannot tell whether all keys belong to one
  person; the social rule (one person per identity) applies as it does today.
- **x/session grants.** The multisig is a granter like any address: create a
  session key once (threshold-signed), then act with the hot key.
- **Gas.** A 2-of-3 tx costs about 14k gas more than a single-key tx and a
  3-of-5 about 25k (extra signatures, the pubkeys and signatures in the tx
  bytes, the larger account record). That fits the default limit but uses half
  of the ~50k headroom a council vote has, so **multisig signers should pass
  `--gas 300000`** (or `--gas auto --gas-adjustment 1.5`) on governance txs.
  Pinned by `app/multisig_ante_test.go`.

## What does not change with a multisig

### Anonymous actions are 1-of-1

A member has two independent credentials. The multisig protects only the first.

| Credential | Used for | Who the chain checks |
|---|---|---|
| Account key (the multisig) | Every signed tx, including `MsgRegisterZkPublicKey` | k-of-n signatures |
| Shield secret | Every `MsgShieldedExec` | A ZK proof of knowing the secret behind a registered trust-tree leaf; the tx is submitted by another address, the member's account never appears |

Whoever holds the shield secret can vote, post, react and challenge anonymously
as the member, within the usual rate limits, with no co-signer involved. This is
inherent to the anonymity design: signing with the account would reveal the
member.

**Where the secret comes from.** Single-key wallets derive it by signing a fixed
message (`DeriveSecretKeyFromSignature`). A multisig has no single key that
could do that, so a naive wallet would tie the anonymous identity to whichever
co-signer device happened to derive it. The rule for multisig members:

- Derive the secret from a **dedicated shield seed**: 256 random bits, shown as a
  24-word mnemonic and backed up like one (`prover.DeriveSecretKeyFromSeed`).
  Never a passphrase: the derivation is an unstretched hash.
- The person decides which devices hold the seed. Losing a co-signer device
  does not lose the anonymous identity.

### Co-signers must check whose key they register

The threshold controls *whether* a key is registered, not *whose* key it is: to
a co-signer, `zk_public_key` is 32 opaque bytes. A compromised device can build
a registration carrying an attacker's key, and co-signers who sign blindly hand
over the member's anonymous identity. Before signing `MsgRegisterZkPublicKey`:

- a device that holds the seed re-derives the key and refuses on a mismatch;
- a device that does not hold the seed compares fingerprints:

```bash
# on the seed-holding device
sparkdreamd zk-key-fingerprint seed "<24-word shield seed>"
# key         3f9a...   fingerprint 7c1e 02ab 9d44 f310

# on a co-signer device, against the unsigned tx it is asked to sign
sparkdreamd zk-key-fingerprint tx register-zk.unsigned.json
# msg 0: member sprkdrm1...   fingerprint 7c1e 02ab 9d44 f310
```

The fingerprint is the first 8 bytes of SHA-256 of the key, hex in groups of
four (`tools/crypto.ZkKeyFingerprint`). `zk-key-fingerprint key <hex>` prints
it for a raw key.

### A leaked shield secret

The holder can act anonymously as the member. They cannot move SPARK or DREAM,
change grants or re-register the key: each of those needs the account key.

Recovery is threshold-signed:

- **`tx rep clear-zk-public-key`** zeroes the leaf (and any pending key) at
  once. It has no cooldown and no per-season limit, because zeroing only takes
  capability away. The member is anonymous-blind until a new key activates.
- **`tx rep register-zk-public-key <new-key>`** afterwards is never immediate:
  the new key waits `zk_key_activation_delay` (14 epochs on the networks), and
  the once-per-season change cap still applies. A member who already changed
  their key this season stays without anonymous access until the next season.
  This is the safe failure: clear-then-register is never a free key swap, so the
  double-action hole a fresh nullifier would open stays closed.

Both messages, like the member-key-migration messages, are on rep's **identity
list** (`reptypes.IdentityMsgTypeURLs()`): never session-delegable (x/session
params refuse them in any allowlist), never shield-compatible. A hot key must
not be able to re-point the anonymous identity around the threshold.

### Signers are fixed for the life of the address

A native multisig address is a hash of its key set, so any signer change
produces a new address. A member rotates a co-signer with a **key-path member
migration** from the old multisig address to a new one (`MsgRequestMigration`
signed by the old set, `MsgAcceptMigration` multisigned by the new set; the
accept window is 7 days to allow for signature collection). The rolling cap of
two key-path migrations per 365 days allows a device replacement plus one
unplanned rotation a year. If the threshold itself is lost, the recovery or
council path applies, exactly as for a lost single key.

Recovery and council migrations clear the ZK key, so a thief cannot keep a
signature-derived shield secret through a recovery.

### Sign-in to services

sdaplogin's ADR-036 sign-in accepts a single secp256k1 key only. A multisig
member creates an x/session **SignIn** grant (threshold-signed) to a single-key
grantee for the service; sdaplogin reads `grants/by_grantee?type=GRANT_TYPE_SIGN_IN`.

## CLI flow

```bash
# 1. keys (each co-signer device holds one; the multisig key is public data)
sparkdreamd keys add k1   # repeat on each device, or import the public keys
sparkdreamd keys add k2
sparkdreamd keys add k3
sparkdreamd keys add ms --multisig k1,k2,k3 --multisig-threshold 2
MS=$(sparkdreamd keys show ms -a)

# 2. fund it (an account must exist before it can sign)
sparkdreamd tx bank send alice "$MS" 100000000uspark.sparkdream --fees 5000uspark.sparkdream -y

# 3. build any tx unsigned
sparkdreamd tx bank send "$MS" <to> 1000000uspark.sparkdream \
  --from ms --generate-only --gas 300000 --fees 50000uspark.sparkdream > unsigned.json

# 4. k co-signers each add a signature (Ledger: add --ledger on that device)
sparkdreamd tx sign unsigned.json --multisig ms --from k1 --output-document k1.sig.json
sparkdreamd tx sign unsigned.json --multisig ms --from k3 --output-document k3.sig.json

# 5. assemble and broadcast (anyone can do this)
sparkdreamd tx multisign unsigned.json ms k1.sig.json k3.sig.json --output-document signed.json
sparkdreamd tx broadcast signed.json
```

Signing offline (`--offline --account-number N --sequence S`) works for every
step. Keplr cannot collect multisig signatures; the CLI flow is the supported
path until a coordinator UI exists.

### Cold root, hot key

The recommended setup: the multisig is the cold root and never signs routine
txs. Once, threshold-signed, it grants a session key:

```bash
sparkdreamd tx session create-session <hot-key-address> \
  "/sparkdream.blog.v1.MsgCreatePost,/sparkdream.forum.v1.MsgCreateReply" \
  50000000uspark.sparkdream 2026-11-01T00:00:00Z 100 \
  --from ms --generate-only --gas 300000 --fees 50000uspark.sparkdream > session.unsigned.json
# sign x2, multisign, broadcast as above
```

The hot key then acts through `MsgExecSession`, pays fees from the multisig
within `spend_limit`, and can be revoked at any time (threshold-signed
`tx session revoke-session`). Spending caps and recurring payments use the
other x/session payloads from the same granter. See
[session-keys.md](session-keys.md).

## Manual Ledger check

Before a release that touches the ante chain or signing, run once with a
Ledger holding one sub-key: build a `MsgVote` or `MsgCreatePost` tx with
`--generate-only`, sign on the Ledger with `tx sign --multisig ms --from
ledger-key --ledger`, collect the second signature from a software key,
multisign and broadcast. The device must display the amino-JSON message and the
tx must land with code 0.

## Tests

- `app/multisig_ante_test.go`: a 2-of-3 account through the full ante chain and
  FinalizeBlock on an e2e snapshot genesis (bank send and session grant with
  mixed sub-signature modes; 1-of-3, a foreign sub-signature and a stale
  sequence refused); steady-state gas overhead of a 3-of-5 pinned under 35k.
- `test/session/multisig_account_test.sh`: the CLI flow end to end, membership,
  ZK key registration with the fingerprint check, clear then re-register,
  hot-key session, a session key refused `MsgRegisterZkPublicKey`, a SignIn
  grant.
- `x/rep/keeper/msg_server_clear_zk_public_key_test.go`,
  `x/rep/types/migration_guard_test.go` (identity list),
  `x/guardian/keeper/msg_server_test.go` (`tx_sig_limit` floor).

## Non-goals

- Shared member identity of any kind: membership is a person.
- A shared account with a stable address and rotatable signers is a different tool: x/vault
  ([x-vault-spec.md](x-vault-spec.md)). A vault can never be a member.
- A k-of-n anonymous proof: it would need a new circuit, a new trusted-setup
  ceremony and an interactive round between co-signers for every anonymous
  action. Anonymous actions stay 1-of-1.
- Account abstraction in the ante handler.

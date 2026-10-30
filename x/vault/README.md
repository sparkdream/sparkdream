# x/vault

Shared accounts with a stable, keyless, module-derived address and a weighted,
rotatable signer set. Signers propose and approve on chain with ordinary
single-signer txs; a proposal executes once the approving weight reaches the
vault's threshold. A vault is never a member and never a council body.
Spec: [docs/x-vault-spec.md](../../docs/x-vault-spec.md).

## Messages

| Msg | Signer | Description |
|---|---|---|
| `MsgCreateVault` | anyone | Burn `creation_fee_amount`, create the vault and its keyless base account; only the creator's own entry starts accepted |
| `MsgAcceptVaultSigner` | listed address | Accept a listing; until then the entry has no weight, cannot act and does not count toward `MaxVaultsPerSigner` |
| `MsgLeaveVault` | listed address | Leave a vault or decline a listing; refused if the vault could no longer reach its threshold |
| `MsgPropose` | current accepted signer | Open a proposal (implicit approval); optional immediate execute; per-proposer pending caps |
| `MsgApprove` | current accepted signer | Approve; `messages_hash` must match; optional immediate execute |
| `MsgRevokeApproval` | approver | Withdraw an approval |
| `MsgExecute` | current accepted signer | Run the proposal once the tally meets the threshold (executor pays gas) |
| `MsgCancelProposal` | proposer, or the vault via a proposal | Cancel a pending proposal |
| `MsgUpdateVault` | the vault address (inner message only) | Replace signers/threshold/TTL/label; bumps `config_version`, invalidates other pending proposals |
| `MsgUpdateParams` | gov | |

Every x/vault message is forbidden from x/session allowlists. Inner messages a
vault may execute exclude rep identity messages, `MsgShieldedExec`,
`MsgExecSession` and every x/vault message except `MsgUpdateVault` /
`MsgCancelProposal`.

## Params

| Name | Type | Default | Description |
|---|---|---|---|
| `creation_enabled` | bool | `true` | Gates `MsgCreateVault` only; never freezes existing vaults |
| `creation_fee_amount` | Int | `10000000` | Burned on creation, bond-denom micro-units |

Compiled bounds (`types/types.go`, `types/bounds_vals_*.go`): `MaxSigners` 10,
`MaxSignerWeight` 1000, `MaxMessagesPerProposal` 16,
`MaxPendingProposalsPerProposer` 8, `MaxPendingUpdateProposalsPerProposer` 2,
`MinProposalTTL` 1 h (10 s test build), `MaxProposalTTL` 30 d,
`ProposalRetention` 7 d (60 s test build), `MaxVaultsPerSigner` 20 (accepted
entries only).

## CLI

```bash
sparkdreamd tx vault create-vault "$A:1,$B:1,$C:1" 2 86400 "phoenix treasury" --from alice
sparkdreamd q vault vaults-by-signer $B --invited                # pending listings
sparkdreamd tx vault accept-vault-signer 1 --from bob           # likewise carol; alice created it
sparkdreamd tx vault propose 1 send.json --from alice          # {"messages":[...],"memo":""}
sparkdreamd q vault tally 1                                     # approved_weight / threshold / met
sparkdreamd tx vault approve 1 <messages_hash_hex> --execute --from bob
sparkdreamd q vault proposals-by-vault 1 --status PROPOSAL_STATUS_PENDING
sparkdreamd q vault vaults-by-signer $B
```

## Wiring

Late-wired from `app.go`: `SetRouter`, `SetRepKeeper` (alias contract +
`IsPersonalAddress`), `SetIdentityKeeper` (bond denom); x/rep gets
`SetVaultChecker(VaultKeeper.IsVault)`. Module account permission: `Burner`.

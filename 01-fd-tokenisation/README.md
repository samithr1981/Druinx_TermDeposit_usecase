# ABC Bank FD Tokenisation — Rebuilt on Drunix (Hyperledger Fabric fork)

This is a functional port of the Chia SpendSim demo (`abcbank_fd_demo.py`) onto
**Drunix**, NPCI's enhanced Hyperledger Fabric fork. It reproduces the full FD
lifecycle: KYC/DID → mint → timelock → clawback → maturity → redemption → audit.

---

## Why this is a *functional* port, not a 1:1 port

The two platforms have fundamentally different trust and execution models, so the
same business behaviour is implemented with different primitives.

| Concern | Chia (original) | Drunix / Fabric (this build) |
|---|---|---|
| Ledger model | UTXO + smart coins (CLVM puzzles) | Permissioned key-value world state |
| Token | CAT coin with a TAIL | `FDToken` ledger record |
| Token identity | CAT TAIL hash | `Symbol` / `ID` field |
| Who can act | Whoever holds the right BLS key | MSP **client identity** (X.509 via Fabric CA) |
| Clawback | Bank BLS signature recalls the coin | Only `BankMSPID` identity may flip status + reassign owner |
| Redemption auth | Depositor BLS signature | Caller's MSP `GetID()` must equal the FD's `DepositorID` |
| Timelock | `ASSERT_HEIGHT_ABSOLUTE` (block height) | Maturity **timestamp** vs `GetTxTimestamp()` — see note below |
| Permissionless? | Yes, public chain | No, permissioned consortium |
| "DataLayer" | Chia DataLayer key/values | `FDRecord` + `Meta` map in world state |
| Audit trail | in-memory `audit[]` list | chaincode `SetEvent` + ledger history |

### Important deviation: block-height timelock → timestamp timelock

Chia enforces the timelock with `ASSERT_HEIGHT_ABSOLUTE`, reading the chain's
block height. **Fabric chaincode cannot deterministically read the committing
block height at endorsement time** — endorsement happens before ordering, so the
final height is unknown. The deterministic, supported equivalent is the
transaction timestamp from `ctx.GetStub().GetTxTimestamp()`, so maturity here is a
**unix timestamp**, not a block number.

> I have NOT been able to compile this chaincode in my environment (no Go
> toolchain / module proxy access here), so please run `go build ./...` and the
> `network.sh deployCC` step before relying on it. The API surface
> (`GetClientIdentity().GetID()/GetMSPID()`, `GetTxTimestamp().Seconds`) was
> verified against the vendored `fabric-contract-api-go/v2` and
> `fabric-chaincode-go/v2` inside the Drunix repo, but verified-against-source is
> not the same as a successful build.

---

## Phase mapping (Chia → Drunix chaincode functions)

| Phase | Chia demo | Drunix function |
|---|---|---|
| 0 Setup | `sim_and_client()`, farm blocks | `network.sh up createChannel`, `deployCC` |
| 1 KYC/DID | `dl.set("depositor/...")` | `RegisterKYC` |
| 2 Mint | `make_spend` CREATE_COIN to depositor | `MintFD` |
| 3 Timelock test | `ASSERT_HEIGHT_ABSOLUTE` guard | `RedeemFD` before maturity → rejected; `TimeToMaturity` to inspect |
| 4 Clawback | bank BLS sign + recall | `ClawbackFD` then `RestoreFD` (AML cleared) |
| 5 Advance time | farm 1,460 blocks | wait until `MaturityTime`, or mint with a short tenor for the demo |
| 6 Redemption | depositor sig + height check, burn | `RedeemFD` |
| 7 DataLayer | `dl.print_store()` | `ReadRecord`, `SetMeta`, `GetAllFDs` |

---

## Prerequisites

- Linux (or WSL2): `git`, Docker, Go 1.23+, `jq`
- The Drunix repo unpacked, with `drunix-network/test-network` available
- Test-network default org `Org1MSP` is treated as **ABC Bank** in the chaincode
  (constant `BankMSPID = "Org1MSP"`). The depositor is modelled as a separate
  client identity enrolled under whichever org you choose to redeem from.

---

## Step-by-step (run ONE command at a time)

Place the `chaincode-go` folder from this package at
`drunix-network/abcfd-tokenisation/chaincode-go` inside the Drunix repo.

1. Bring up the test network and create the channel:

```bash
./network.sh up createChannel -c mychannel
```

2. Deploy the FD chaincode:

```bash
./network.sh deployCC -ccn abcfd -ccp ../abcfd-tokenisation/chaincode-go -ccl go -c mychannel
```

3. Export the peer env for **ABC Bank (Org1)** — set these one at a time:

```bash
export PATH=${PWD}/../bin:$PATH
```

```bash
export FABRIC_CFG_PATH=$PWD/../config/
```

```bash
export CORE_PEER_TLS_ENABLED=true
```

```bash
export CORE_PEER_LOCALMSPID="Org1MSP"
```

```bash
export CORE_PEER_TLS_ROOTCERT_FILE=${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt
```

```bash
export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp
```

```bash
export CORE_PEER_ADDRESS=localhost:7051
```

4. **PHASE 1 — Register KYC** (bank acting):

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"RegisterKYC","Args":["ABCFD-001","Mr. Ramesh Kumar","ABCPK1234X","did:drunix:abcbank:ramesh","Mrs. Sunita Kumar","did:drunix:abcbank:sunita"]}'
```

5. **PHASE 2 — Mint the FD.** Use a SHORT tenor (e.g. 60 seconds) for the demo so
   you can reach maturity quickly. The `depositorID` must be the depositor's MSP
   client ID string — get it from the depositor's cert (see note after the
   commands). For a first run you can mint with the bank's own ID and redeem as
   the bank to confirm the happy path, then redo with a real depositor identity.

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"MintFD","Args":["ABCFD-001","ABCFD","3000000","232808","60","<DEPOSITOR_CLIENT_ID>","did:drunix:abcbank:ramesh"]}'
```

6. **PHASE 3 — Timelock test.** Attempt redemption immediately as the depositor.
   It must be rejected because the maturity timestamp has not passed:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"RedeemFD","Args":["ABCFD-001"]}'
```

7. **PHASE 4 — Clawback** (bank), then restore once AML clears:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"ClawbackFD","Args":["ABCFD-001","AML_REVIEW"]}'
```

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"RestoreFD","Args":["ABCFD-001"]}'
```

8. **PHASE 5 — Advance time.** With a 60-second tenor, just wait ~70 seconds.

```bash
sleep 70
```

9. **PHASE 6 — Maturity redemption** (must run as the depositor identity; see
   note below). Now it should succeed and return the payout:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n abcfd --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"RedeemFD","Args":["ABCFD-001"]}'
```

10. **PHASE 7 — Inspect ledger** (query, no invoke):

```bash
peer chaincode query -C mychannel -n abcfd -c '{"function":"ReadFD","Args":["ABCFD-001"]}'
```

```bash
peer chaincode query -C mychannel -n abcfd -c '{"function":"GetAllFDs","Args":[]}'
```

11. Tear down:

```bash
./network.sh down
```

---

## Getting the depositor's client ID

`RedeemFD` checks that the caller's `GetID()` equals the FD's `DepositorID`. The
ID string has the form `x509::<subject>::<issuer>`. To enrol a separate depositor
identity and read its ID you would register a new user with the org's Fabric CA,
then either compute the ID from the cert or add a temporary `WhoAmI` query
function to the chaincode that returns `ctx.GetClientIdentity().GetID()`. I did
not add `WhoAmI` to keep the contract minimal — say the word and I will.

For a quick happy-path smoke test, mint with the bank's own client ID as
`<DEPOSITOR_CLIENT_ID>` and run redemption with the same Org1 admin context.

---

## What is faithfully reproduced vs. simplified

Faithful: bank-only mint/clawback, identity-gated redemption, pre-maturity
redemption rejection, clawback→restore cycle, burn-on-redemption, KYC/DataLayer
records, event emission for audit.

Simplified (call out before any pilot use):
- No real token *transfer* between holders is implemented (the Chia demo also
  doesn't move the CAT between third parties).
- Interest is a stored fixed figure, not computed on-chain.
- The depositor identity wiring is left to you (Fabric CA enrolment), as it
  depends on your org topology.
- Maturity is timestamp-based, not block-height-based (Fabric constraint).

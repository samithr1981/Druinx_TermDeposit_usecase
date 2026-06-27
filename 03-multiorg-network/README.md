# Drunix — Genuine Two-Peer, Two-Org Network Demo

This demo proves a **real multi-organisation Drunix network**: two organisations
(`Org1MSP`, `Org2MSP`), each running their own peers, jointly own one ledger.
A chaincode is deployed with an endorsement policy that **requires BOTH orgs to
sign every state change**. An asset created by Org1 is transferred to Org2, and
querying from either org returns identical data — the shared, replicated ledger
is the proof.

Unlike the earlier single-org demos, here the multi-org property is real, not
simulated: the two peers belong to different MSPs, run in separate containers,
and must independently endorse a transaction before it commits.

---

## What the Drunix test-network actually runs

This is **not** stock Hyperledger Fabric. Drunix splits the monolithic Fabric
peer into three cooperating roles per org (confirmed from
`compose/compose-test-net.yaml`):

| Role | Container | Org1 port | What it does |
|---|---|---|---|
| **Lite Peer (LP)** | `lp1.org1` / `lp1.org2` | 7051 / 9051 | Front door: endorses proposals, serves queries. This is the address clients talk to. |
| **Committing Peer (CP)** | `cp.org1` / `cp.org2` | 7061 | Writes validated blocks to the ledger / state DB. |
| **VSSC / Validation Service** | `vs1.org1` / `vs1.org2` | 7071 | Stateless transaction validation, pulled out of the peer. |
| State DB | `yugabyte-org1/2` | 5433 | SQL (YugabyteDB) on-chain state — a Drunix enhancement over LevelDB/CouchDB. |
| Transient store | `hlf_keydb_org1msp` | 6379 | KeyDB-backed transient/private data store. |

Plus a single etcdraft **orderer** (`orderer.example.com:7050`) shared by both
orgs. So a genuine run has, at minimum: 1 orderer + (LP+CP+VSSC)×2 orgs +
2 state DBs + 2 transient stores. The two orgs are symmetric.

Why this matters for "multi-org": each org validates independently through *its
own* LP and VSSC. When the endorsement policy says "both orgs must sign", Org1's
LP and Org2's LP each run the chaincode against their own copy of the ledger and
sign the result. Only if both signatures agree does the orderer cut a block and
both CPs commit it. That is the trust model of a real consortium chain.

---

## The flow this demo exercises

```
                          ┌─────────── ORDERER (etcdraft) ───────────┐
                          │        orderer.example.com:7050           │
                          └───────▲───────────────────────▲──────────┘
                                  │ ordered block         │
        endorse (sign)           │                        │  endorse (sign)
   ┌──────────────────┐          │                        │     ┌──────────────────┐
   │  ORG1            │          │                        │     │  ORG2            │
   │  LP lp1.org1:7051│──────────┘                        └─────│  LP lp1.org2:9051│
   │  VSSC vs1.org1   │   both signatures required by policy     │  VSSC vs1.org2   │
   │  CP  cp.org1     │◀── commit block ──┐      ┌── commit ─────▶│  CP  cp.org2     │
   │  YugabyteDB org1 │                   │      │                │  YugabyteDB org2 │
   └──────────────────┘                   └──────┘                └──────────────────┘
        Org1 ledger copy  ◀══════ identical state ══════▶  Org2 ledger copy
```

Endorsement policy used: **`AND('Org1MSP.peer','Org2MSP.peer')`** — both orgs
must approve.

> Build status: I could not compile the Go chaincode in my environment (no Go
> toolchain / module-proxy access here). The API calls used —
> `GetClientIdentity().GetMSPID()`, `GetStateByRange`, `SetEvent` — were verified
> against the `fabric-chaincode-go/v2` and `fabric-contract-api-go/v2` vendored
> inside the Drunix repo. Run `go build ./...` then `deployCC` before relying on
> it. The network/topology facts above are read directly from the repo's compose
> file.

---

## Run it (one command at a time)

Place `chaincode-go` at `drunix-network/multiorg-registry/chaincode-go`, then
work from `drunix-network/test-network`.

### 1. Bring up BOTH orgs and create the channel

```bash
./network.sh up createChannel -c mychannel
```

This starts the orderer and the LP/CP/VSSC trio for **both** Org1 and Org2, then
creates `mychannel` and joins both orgs' peers to it.

### 2. Deploy chaincode requiring BOTH orgs to endorse

```bash
./network.sh deployCC -ccn registry -ccp ../multiorg-registry/chaincode-go -ccl go -c mychannel -ccep "AND('Org1MSP.peer','Org2MSP.peer')"
```

The `-ccep` flag sets the endorsement policy. With `AND(...)`, no single org can
change state alone — this is what makes the network genuinely multi-org rather
than two copies of a single-org chain.

### 3. Set the shell to act as Org1 (one at a time)

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

### 4. Org1 creates an asset — note BOTH peers are named as endorsers

The invoke lists `--peerAddresses` for **both** Org1 (7051) and Org2 (9051). The
gateway collects an endorsement from each, satisfying the AND policy:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n registry --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" --peerAddresses localhost:9051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt" -c '{"function":"CreateAsset","Args":["trade-001","500"]}'
```

### 5. Query from Org1 (the creator's view)

```bash
peer chaincode query -C mychannel -n registry -c '{"function":"ReadAsset","Args":["trade-001"]}'
```

You should see `OwnerOrg: Org1MSP`, `CreatedByOrg: Org1MSP`.

### 6. Transfer the asset from Org1 to Org2 (crosses the org boundary)

Still acting as Org1 (the current owner), again endorsed by both peers:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n registry --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" --peerAddresses localhost:9051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt" -c '{"function":"TransferAsset","Args":["trade-001","Org2MSP"]}'
```

### 7. Switch the shell to act as Org2 (one at a time)

```bash
export CORE_PEER_LOCALMSPID="Org2MSP"
```

```bash
export CORE_PEER_TLS_ROOTCERT_FILE=${PWD}/organizations/peerOrganizations/org2.example.com/peers/peer0.org2.example.com/tls/ca.crt
```

```bash
export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org2.example.com/users/Admin@org2.example.com/msp
```

```bash
export CORE_PEER_ADDRESS=localhost:9051
```

### 8. Query the SAME asset from Org2's peer

```bash
peer chaincode query -C mychannel -n registry -c '{"function":"ReadAsset","Args":["trade-001"]}'
```

This reads from Org2's **own ledger copy** (different container, different state
DB), and returns `OwnerOrg: Org2MSP`. Identical data, independently held — the
multi-org consensus worked.

### 9. Confirm which org you are, any time

```bash
peer chaincode query -C mychannel -n registry -c '{"function":"WhoAmI","Args":[]}'
```

### 10. Tear down

```bash
./network.sh down
```

---

## How to *prove* it's really two orgs (not theatre)

1. **Drop one endorser and watch it fail.** Re-run step 4 with only the Org1
   `--peerAddresses` (omit the 9051 pair). With the `AND` policy the proposal
   collects just one signature and the commit is rejected — demonstrating Org2's
   approval is genuinely required.
2. **Inspect the containers.** `docker ps` shows `lp1.org1`, `cp.org1`,
   `vs1.org1`, `lp1.org2`, `cp.org2`, `vs1.org2`, two `yugabyte-*` and two
   `hlf_keydb_*` containers — separate processes per org.
3. **Query each org's LP independently** (steps 5 and 8) and compare. Same asset,
   same value, retrieved from two different ledger copies.

---

## What's simplified

- Two orgs is the canonical consortium minimum; the same pattern scales to N orgs
  by adding org definitions and extending the endorsement policy.
- The asset model is deliberately tiny so the focus stays on the multi-org
  mechanics. The richer FD / P2P chaincodes from the earlier demos can be
  deployed onto this exact two-org network by swapping the `-ccp` path and using
  the same `-ccep "AND('Org1MSP.peer','Org2MSP.peer')"` policy.
- Channel and crypto material use the test-network defaults (cryptogen). A
  production consortium would use Fabric CA and real org governance.

See `multiorg-walkthrough.html` for a visual, click-through version of this flow.

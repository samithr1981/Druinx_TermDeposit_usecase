# Drunix FD Tokenisation — Verified Working Notes

**Status: VERIFIED WORKING.** This chaincode was deployed end-to-end on a live,
two-organization Drunix test-network and every lifecycle phase was independently
confirmed by querying the ledger's own state (not just invoke-success messages).
This document is the first fully working use case built on Drunix.

This document is written to be **queried by a support agent or AI assistant** —
each section is self-contained, uses consistent terminology, and the
Troubleshooting section is in Q&A form. If you're an agent answering a question
about this deployment, search the section headers and the Q&A block below
before answering from general knowledge — the specific errors and fixes here
were only discovered through an actual deployment, not documented anywhere else.

---

## 1. What this is

A Fixed Deposit (FD) tokenisation smart contract (chaincode) running on
**Drunix**, NPCI's enhanced fork of Hyperledger Fabric. It implements the full
FD lifecycle: KYC registration, minting, timelocked redemption, bank clawback,
clawback restoration, and maturity redemption — as Go chaincode functions
enforced by MSP identity checks and a two-organization endorsement policy.

It was originally designed as a functional port of an earlier Chia
(UTXO/smart-coin) demo. The architectural differences between the two models
are documented in the main project README, not repeated here.

## 2. Verified environment

This exact sequence was run and verified on:
- macOS, Apple Silicon (arm64)
- Docker Desktop 4.80.0 (build 232116)
- Go 1.26.0 (darwin/arm64) — chaincode module itself targets Go 1.23+
- jq 1.6

**Not verified on**: Linux, Intel Macs, Windows/WSL. The steps below should be
similar, but only the above combination has actually been exercised.

**Note on emulation**: Drunix's published Docker images
(`npcioss/drunix-peer:1.0.0`, `npcioss/drunix-orderer:1.0.0`,
`npcioss/drunix-vscc:1.0.0`) are built for `linux/amd64` only, confirmed via
`docker image inspect ... --format '{{.Architecture}}'`. On Apple Silicon these
run under Docker Desktop's Rosetta emulation. This worked without a platform
mismatch error blocking the pull, but may be slower than a native amd64 host.

## 3. Prerequisites, exactly as verified

```bash
docker --version   # confirmed working: Docker version 29.6.1
go version          # confirmed working: go1.26.0 darwin/arm64
jq --version         # confirmed working: jq-1.6
```

Docker Desktop installation on macOS via Homebrew can fail with
`Error: It seems there is already a Binary at '/usr/local/bin/docker'` (or
similarly named binaries like `docker-credential-osxkeychain`,
`docker-credential-desktop`, `cagent`, `docker-compose`, `hub-tool`, `kubectl`,
`kubectl.docker`). This happens when a previous, incomplete Docker install left
dangling symlinks pointing at a `/Applications/Docker.app` that no longer
exists. Fix verified in this session:

```bash
ls -la /usr/local/bin/docker              # confirm it's a symlink owned by root
sudo rm /usr/local/bin/docker              # repeat for each conflicting binary
                                           # Homebrew reports one at a time
brew install --cask docker-desktop         # retry after each removal
```

## 4. Getting the Drunix source

```bash
git clone https://github.com/npci/drunix.git
```

This is a public GitHub repository under the `npci` organization. Confirmed
present at time of this deployment: a `drunix-network/test-network` directory
with `network.sh`, matching the structure this document assumes throughout.

## 5. Building the native binaries (required before `network.sh up`)

`network.sh up` requires `peer`, `orderer`, and supporting CLI tools as native
binaries on your `PATH` — these are NOT provided by the Docker images alone.

```bash
cd drunix
make native
```

This builds `orderer, configtxgen, configtxlator, cryptogen, discover,
ledgerutil, osnadmin, peer, vscc` into `build/bin/`, confirmed via
`RELEASE_EXES = orderer $(TOOLS_EXES)` and `TOOLS_EXES` in the repo's Makefile.
Verify:

```bash
./build/bin/peer version
```

Confirmed output included `OS/Arch: darwin/arm64` — a **native** ARM64 binary,
distinct from the amd64 Docker images used for the actual network containers.

## 6. Starting dependency services NOT started by `network.sh` — critical gap

**This is the single most important undocumented gap found in this session.**
`network.sh up createChannel` brings up the orderer and all six
peer/vscc containers (`compose/compose-test-net.yaml`), but that compose file
does **not** define YugabyteDB or KeyDB — even though the peers' own
environment variables reference them
(`CORE_PEER_KVSTORE_ADDRESS=hlf_keydb_org1msp:6379`,
`CORE_LEDGER_STATE_SQLDBCONFIG_ADDRESS=yugabyte-org1`). These live in a
**separate** compose file: `scripts/yugabyte/compose.yaml`.

If you skip this step, every peer/vscc container will crash on startup with:
```
Error: failed to initialise key-value db for kv store: dial tcp: lookup hlf_keydb_org1msp on 127.0.0.11:53: no such host
```

**Fix — run this BEFORE `network.sh up`, or immediately after if peers have
already crashed:**

```bash
cd drunix-network/test-network
docker compose -f scripts/yugabyte/compose.yaml up -d
```

This starts `yugabyte-org1`, `yugabyte-org2`, `hlf_keydb_org1msp`,
`hlf_keydb_org2msp` on the same `drunix_test` Docker network (the compose file
declares `networks: test: name: drunix_test`, matching the network
`network.sh` creates). If peers already crashed because this step was missed,
restart them after Yugabyte is confirmed up:

```bash
docker start lp1.org1 cp.org1 vs1.org1 lp1.org2 cp.org2 vs1.org2
```

## 7. Bringing up the network

```bash
./network.sh up createChannel -c mychannel
```

Confirmed working, with the dependency fix from section 6 applied first (or
peers restarted after). This creates the genesis block, creates `mychannel`,
and joins the **Committing Peers** (`cp.org1`, `cp.org2`) to the channel
automatically.

## 8. Critical gap: Lite Peers are NOT auto-joined to the channel

Drunix splits each org's peer role into three: **Lite Peer** (`lp1.orgN`, ports
7051/9051 — the client-facing endorser/query peer), **Committing Peer**
(`cp.orgN`, ports 7061/9061 — writes validated blocks), and **VSSC**
(`vs1.orgN`, stateless validation service, ports 7071/9071).

`network.sh`'s automated `createChannel` flow only joins the **Committing
Peer** to the channel. The **Lite Peer** — which is where chaincode is
installed and where client invokes/queries are actually directed — is left
unjoined. This causes `querycommitted`/query failures with
`channel 'mychannel' not found` when targeting the Lite Peer, even though the
Committing Peer shows the channel correctly.

**Fix — join each org's Lite Peer manually:**

```bash
export PATH="$(pwd)/../../build/bin:$PATH"
export FABRIC_CFG_PATH="$(pwd)/../config"
. scripts/envVar.sh

setGlobals 1 0   # org 1, peer index 0 = Lite Peer
peer channel join -b ./channel-artifacts/mychannel.block

setGlobals 2 0   # org 2, peer index 0 = Lite Peer
peer channel join -b ./channel-artifacts/mychannel.block
```

`setGlobals <org> <peer>` takes two arguments; peer index `0` = Lite Peer,
`1` (the default if omitted) = Committing Peer. Confirmed exact mapping by
reading `scripts/envVar.sh` directly in this session.

## 9. Required environment variables for `peer` CLI commands

```bash
export PATH="$(pwd)/../../build/bin:$PATH"      # peer binary not found otherwise
export FABRIC_CFG_PATH="$(pwd)/../config"        # core.yaml not found otherwise
```

Confirmed: `FABRIC_CFG_PATH` must point to `drunix-network/config` specifically
(there are multiple `core.yaml` files in the repo — `sampleconfig/core.yaml`,
two under `compose/{docker,podman}/peercfg/`, and one under
`drunix-network/config/peer/` — the one that worked in this session was
`drunix-network/config/core.yaml`, referenced relative to `test-network` as
`../config`).

## 10. Deploying the chaincode

```bash
cd drunix-network/test-network
./network.sh deployCC -ccn fdtoken -ccp /absolute/path/to/01-fd-tokenisation/chaincode-go -ccl go -c mychannel
```

**Prerequisite: `go.sum` must exist in the chaincode directory.** If it's
missing, the Docker-based chaincode build fails with:
```
missing go.sum entry for module providing package github.com/hyperledger/fabric-contract-api-go/v2/contractapi
```
Fix (run once, on a machine with internet access):
```bash
cd 01-fd-tokenisation/chaincode-go
go mod tidy
```
This generates `go.sum` from the versions already pinned in `go.mod`
(`fabric-contract-api-go/v2 v2.2.0`, `fabric-chaincode-go/v2 v2.0.0` — both
confirmed resolvable on the public Go module proxy in this session).
**`go.sum` must be committed to version control** — it is not safe to
`.gitignore` for a Go chaincode module intended to be built by others.

`deployCC` with `CC_SEQUENCE=auto` (the default) correctly auto-increments the
sequence number on redeploys — confirmed across four redeploys in this
session (sequences 1 through 4) without manual intervention.

## 11. Invoking the chaincode — use Lite Peer addresses

Confirmed: chaincode is installed and reachable for invoke/query via the
**Lite Peer** addresses (`localhost:7051` for Org1, `localhost:9051` for
Org2) — NOT the Committing Peer addresses (7061/9061), even though the
`deployCC` script's own internal install/approve/commit steps use Committing
Peer addresses for the lifecycle-management calls themselves. This distinction
tripped up an early invoke attempt in this session (error:
`chaincode definition for 'fdtoken' exists, but chaincode is not installed`)
until the peer addresses were corrected to 7051/9051.

Example invoke pattern (both peer addresses required — the default 2-org
policy requires both orgs to endorse):

```bash
peer chaincode invoke -o localhost:7050 \
  --ordererTLSHostnameOverride orderer.example.com --tls \
  --cafile <path>/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem \
  -C mychannel -n fdtoken \
  --peerAddresses localhost:7051 --tlsRootCertFiles <path>/organizations/peerOrganizations/org1.example.com/tlsca/tlsca.org1.example.com-cert.pem \
  --peerAddresses localhost:9051 --tlsRootCertFiles <path>/organizations/peerOrganizations/org2.example.com/tlsca/tlsca.org2.example.com-cert.pem \
  -c '{"function":"<FunctionName>","Args":[...]}'
```

## 12. Getting a caller's client ID (for `depositorID` arguments)

`MintFD` requires the depositor's MSP client identity string as an argument.
This chaincode includes a `WhoAmI` query function specifically for discovering
this value during testing:

```bash
peer chaincode query -C mychannel -n fdtoken -c '{"function":"WhoAmI","Args":[]}'
```

The result is **base64-encoded**. Decode it to get the human-readable form:

```bash
echo "<base64 output>" | base64 -d
```

Confirmed decoded format: `x509::CN=Admin@org1.example.com,OU=admin,...::CN=ca.org1.example.com,...`
— pass the **original base64 string** (not the decoded form) as the
`depositorID` argument, since that is the exact string `GetClientIdentity().GetID()`
returns internally and what later redemption checks compare against.

## 13. Verified full lifecycle — actual results from this session

All results below are copied directly from actual chaincode queries run
against the live network, not illustrative examples.

**Phase 1 — KYC registration** (`RegisterKYC`), verified via `ReadRecord`:
```json
{"DepositorDID":"did:drunix:abcbank:ramesh","DepositorName":"Ramesh Kumar","DepositorPAN":"ABCPK1234X","DocType":"fdrecord","ID":"ABCFD-001","KYCStatus":"VERIFIED","Meta":{},"NomineeDID":"did:drunix:abcbank:sunita","NomineeName":"Sunita Kumar"}
```

**Phase 2 — Minting** (`MintFD`, principal 3,000,000 units, interest 232,808,
60-second tenor), verified via `ReadFD`:
```json
{"Amount":3000000,"...","MaturityUnits":3232808,"Status":"ACTIVE",...}
```

**Phase 3 — Timelock enforcement**, early `RedeemFD` correctly rejected:
```
Error: endorsement failure during invoke. response: status:500 message:"timelock active: FD ABCFD-001 matures at 1783444441, current tx time 1783444428 (13 seconds remaining)"
```

**Phase 4 — Clawback** (`ClawbackFD`, on a second FD `ABCFD-002`), verified via
`ReadFD`:
```json
{"...","ClawbackReason":"AML_REVIEW_FLAGGED","Status":"CLAWBACK_INITIATED",...}
```

**Phase 4b — Restore** (`RestoreFD`), verified via `ReadFD`:
```json
{"...","ClawbackReason":"","Status":"ACTIVE",...}
```

**Phase 6 — Maturity redemption** (`RedeemFD` after the timelock expired),
verified via `ReadFD`:
```json
{"Amount":0,"...","Status":"REDEEMED",...}
```

## 14. Known simplifications in this verified run

- **One identity played all roles** (bank/minter and depositor) — the Org1
  admin identity was used for both `MintFD`'s bank-authorization check and as
  the `depositorID` value. This is a disclosed simplification for a
  proof-of-concept; the chaincode's identity checks (`msp != BankMSPID`,
  `callerID != token.DepositorID`) are genuinely enforced in the code, but a
  *distinct* second identity (a real depositor separate from the bank) was not
  exercised in this session.
- No performance, load, or security testing has been done. This confirms
  functional correctness of the lifecycle logic, not production-readiness.
- A `scripts/envVar.sh: line 99: [: too many arguments` bash warning appeared
  during chaincode install steps. It did not block execution and appears to
  be a pre-existing minor bug in the vendored script, unrelated to the
  chaincode itself.

---

## 15. Troubleshooting Q&A (for agent retrieval)

**Q: Peer containers crash immediately with "lookup hlf_keydb_org1msp ... no such host". What's wrong?**
A: YugabyteDB and KeyDB were never started. Run
`docker compose -f scripts/yugabyte/compose.yaml up -d` from `test-network`
before (or after, followed by `docker start` on the crashed peers) bringing up
the main network. See Section 6.

**Q: `peer channel list` shows nothing / `querycommitted` says "channel not found", but the Committing Peer shows it fine. Why?**
A: `network.sh` only auto-joins the Committing Peer, not the Lite Peer. Run
`setGlobals <org> 0` then `peer channel join -b ./channel-artifacts/mychannel.block`
for each org. See Section 8.

**Q: `deployCC` fails with "missing go.sum entry for module providing package ...contractapi". What's wrong?**
A: The chaincode's `go.sum` file is missing or incomplete. Run `go mod tidy`
inside the chaincode directory on a machine with internet access, then retry.
See Section 10.

**Q: Chaincode invoke fails with "chaincode definition for 'X' exists, but chaincode is not installed", even though deployCC reported success. Why?**
A: You're targeting the wrong peer address. Chaincode installs onto Lite Peers
(ports 7051/9051), not Committing Peers (7061/9061). Use the Lite Peer
addresses in `--peerAddresses` for invokes/queries. See Section 11.

**Q: A query fails with "Timed out waiting kResponseSent, state: kProcessingRequest (SQLSTATE XX000)". What's wrong?**
A: In this session, this was traced to peer containers having crashed and
been restarted without YugabyteDB being fully ready first (a variant of the
Section 6 issue after a `docker restart` of all containers simultaneously).
Fix: ensure YugabyteDB containers are confirmed "Up" and stable for at least a
minute before starting/restarting peer containers, and restart peers
separately from the database containers rather than all at once.

**Q: `peer` command not found, or "Fatal error when initializing core config: Config File 'core' Not Found". What's wrong?**
A: Missing environment variables in the current shell. Run
`export PATH="<repo>/build/bin:$PATH"` and
`export FABRIC_CFG_PATH="<repo>/drunix-network/config"`. See Sections 5 and 9.

**Q: How do I get a specific identity's client ID to use as a depositorID?**
A: Call the chaincode's `WhoAmI` query function (bank-context only — no
restriction on WhoAmI itself) and base64-decode the result to read it, but use
the original base64 string as the argument value. See Section 12.

**Q: What is Drunix's peer role split, exactly?**
A: Each org runs three containers: Lite Peer (`lp1.orgN`, endorse + serve
queries, client-facing), Committing Peer (`cp.orgN`, validates + writes
blocks), and VSSC (`vs1.orgN`, stateless transaction validation, a Drunix
addition over stock Hyperledger Fabric). Confirmed from
`compose/compose-test-net.yaml` and observed container behavior throughout
this deployment.

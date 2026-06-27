# Drunix Demos — Tokenisation, P2P Transfer, and Multi-Org Network

Three worked examples on **[Drunix](https://github.com/npci/drunix)**, NPCI's
enhanced fork of Hyperledger Fabric. Each demo is a self-contained Go chaincode
plus a step-by-step deployment guide for the Drunix `test-network`.

| # | Demo | What it shows |
|---|------|---------------|
| 01 | **FD Tokenisation** | A fixed-deposit lifecycle (KYC → mint → timelock → clawback → maturity → redemption), ported from a Chia/UTXO design to Drunix's account/key-value model. |
| 02 | **P2P Transfer** | Person-to-person value transfer: account balances, atomic debit/credit, MSP-identity-gated transfers, on-ledger audit trail. |
| 03 | **Multi-Org Network** | A genuine two-organisation network where both orgs must endorse every state change (`AND('Org1MSP.peer','Org2MSP.peer')`), demonstrating Drunix's segregated peer roles (Lite Peer / Committing Peer / VSSC). |
| 04 | **Web Front End** | A working web app (Express + Fabric Gateway SDK) that drives the live two-org network from demo 03 — create / transfer / query, with both orgs' ledger copies shown side by side. |

Each folder has its own `README.md` with the exact `network.sh` and
`peer chaincode` commands.

## Quick start

All three deploy onto the Drunix test network. From a clone of the Drunix repo:

```bash
cd drunix-network/test-network
./network.sh up createChannel -c mychannel
```

Then follow the per-demo README to deploy that demo's chaincode.

## Important notes

- **Not yet compiled or run end-to-end.** The chaincode API calls were verified
  against the `fabric-chaincode-go/v2` and `fabric-contract-api-go/v2` libraries
  vendored inside the Drunix repo, and the multi-org topology facts were read
  from the repo's `compose-test-net.yaml`. But these have not been executed in a
  live network. Run `go build ./...` and `./network.sh deployCC ...` to verify
  before relying on them.
- Demo 01 ports a Chia smart-coin design to Fabric's account model; demo 01's
  README explains the deliberate differences (notably block-height timelock →
  timestamp timelock, a Fabric constraint).

## Licence

Apache-2.0, matching the upstream Drunix / Hyperledger Fabric licence.

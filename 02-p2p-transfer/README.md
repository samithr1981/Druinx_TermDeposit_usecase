# Drunix Peer-to-Peer Token Transfer — Demo

A minimal person-to-person value transfer running on **Drunix**, NPCI's enhanced
Hyperledger Fabric fork. Two parties hold INR-denominated token balances and send
value directly to each other. Transfer rules are enforced by chaincode + MSP
identity.

## The flow

```
  Org1 (issuer/treasury)
        │  Mint
        ▼
  ramesh  ── Transfer 50,000 ──▶  sunita
   (1,00,000)                      (50,000)
        │                              │
        └────── every move writes an immutable receipt ──────┘
                         (GetAllTransfers)
```

## How it maps to Drunix's model

This is an **account / balance** design — the natural shape for Fabric's
key-value world state. Each wallet is an `Account` record `{ID, Owner, Balance}`.
A transfer **atomically** debits the sender's record and credits the receiver's
record inside one transaction: both writes commit, or neither does. There are no
coins to split or merge — value is just a number moved between two rows.

Authorisation comes from **MSP identity**, not a private key sealed in a coin:
- `Transfer` succeeds only if the caller's client ID (`GetID()`) equals the
  sender account's `Owner`.
- `Mint` is restricted to the issuer org (`MinterMSPID = "Org1MSP"`).

### Functions

| Function | Who | Purpose |
|---|---|---|
| `OpenAccount(id)` | anyone | create a wallet owned by the caller |
| `Mint(id, amount)` | Org1 only | issue new tokens into a wallet |
| `Transfer(from, to, amount)` | owner of `from` | the core P2P move |
| `BalanceOf(id)` / `ReadAccount(id)` | anyone | read balance |
| `GetAllAccounts()` | anyone | list wallets |
| `GetAllTransfers()` | anyone | audit trail of all transfers |
| `WhoAmI()` | anyone | returns the caller's MSP client ID |

> Build status: I could not compile this in my environment (no Go toolchain /
> module-proxy access). The API surface — `GetClientIdentity().GetID()/GetMSPID()`,
> `GetTxID()`, `GetStateByRange`, `SetEvent`, `GetTxTimestamp().Seconds` — was
> verified against the `fabric-chaincode-go/v2` and `fabric-contract-api-go/v2`
> vendored inside the Drunix repo. Please run `go build ./...` and `deployCC`
> before relying on it.

---

## Run it (one command at a time)

Place `chaincode-go` at `drunix-network/p2p-transfer/chaincode-go` in the repo,
then from `drunix-network/test-network`:

1. Start the network and channel:

```bash
./network.sh up createChannel -c mychannel
```

2. Deploy:

```bash
./network.sh deployCC -ccn p2p -ccp ../p2p-transfer/chaincode-go -ccl go -c mychannel
```

3. Set the peer env for Org1 (the issuer) — one at a time:

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

4. Open two accounts (as Org1 admin, who becomes the owner of both for this
   simple single-identity demo):

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n p2p --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"OpenAccount","Args":["ramesh"]}'
```

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n p2p --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"OpenAccount","Args":["sunita"]}'
```

5. Mint 1,00,000 tokens to ramesh:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n p2p --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"Mint","Args":["ramesh","100000"]}'
```

6. **The P2P transfer** — ramesh sends 50,000 to sunita:

```bash
peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls --cafile "${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem" -C mychannel -n p2p --peerAddresses localhost:7051 --tlsRootCertFiles "${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt" -c '{"function":"Transfer","Args":["ramesh","sunita","50000"]}'
```

7. Check balances (query — no invoke):

```bash
peer chaincode query -C mychannel -n p2p -c '{"function":"BalanceOf","Args":["ramesh"]}'
```

```bash
peer chaincode query -C mychannel -n p2p -c '{"function":"BalanceOf","Args":["sunita"]}'
```

8. See the audit trail:

```bash
peer chaincode query -C mychannel -n p2p -c '{"function":"GetAllTransfers","Args":[]}'
```

9. Tear down:

```bash
./network.sh down
```

---

## Showing real two-party authorisation (optional, more advanced)

The single-identity run above proves the mechanics. To demonstrate that **only
the owner can spend**, enrol a second identity (e.g. via Org2's Fabric CA), open
an account whose `Owner` is that identity, and try to `Transfer` from it while
using Org1's context — the chaincode will reject it with "caller is not the
owner". Use `WhoAmI` to read each identity's client ID string so you know which
`Owner` value to expect. I left the CA-enrolment steps out because they depend on
your org topology; tell me your setup and I'll write them out exactly.

---

## What's deliberately simplified

- No overdraft, fees, or limits (easy to add as guards in `Transfer`).
- Balances are plain integers in world state, not private/confidential. For
  privacy between counterparties you'd move balances into a private data
  collection (the `asset-transfer-private-data` sample shows the pattern).
- Single channel, single token. Multi-token would add a `Symbol` dimension.

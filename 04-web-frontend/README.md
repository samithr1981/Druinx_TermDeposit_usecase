# Drunix Multi-Org Front End

A small web app that drives the **live** Drunix two-org network through the
Fabric Gateway SDK. It exposes create / transfer / query over HTTP and shows the
two orgs' ledger copies side by side, so you can watch both update in lockstep
after a commit.

This is a real client, not an illustration — it opens gRPC connections to the
running peers using the test-network's crypto material and submits actual
transactions. (The earlier `03-multiorg-network/multiorg-walkthrough.html` is the
static explainer; this is the working app.)

```
 browser  ──HTTP──▶  Express (server/app.js)  ──gRPC──▶  Org1 peer lp1.org1:7051
                                              ──gRPC──▶  Org2 peer lp1.org2:9051
                         uses @hyperledger/fabric-gateway ^1.10.0
```

## Verified against the Drunix repo

The connection code in `server/connection.js` is adapted from the repo's own
sample at
`drunix-network/asset-transfer-basic/application-gateway-javascript/src/app.js`.
Verified from that sample: the SDK versions (`@hyperledger/fabric-gateway`
`^1.10.0`, `@grpc/grpc-js` `^1.14.0`, Node ≥20), the `connect({ client, identity,
signer, hash: hash.sha256 })` pattern, the gRPC TLS setup with
`grpc.ssl_target_name_override`, and reading identity from `signcerts` / signer
from `keystore`. Peer ports (7051 / 9051) come from the repo's
`compose-test-net.yaml`.

## Prerequisites

1. The Drunix `test-network` is **up**, the channel exists, and the `registry`
   chaincode from `03-multiorg-network` is **deployed with the two-org policy**:

   ```bash
   ./network.sh up createChannel -c mychannel
   ```

   ```bash
   ./network.sh deployCC -ccn registry -ccp ../multiorg-registry/chaincode-go -ccl go -c mychannel -ccep "AND('Org1MSP.peer','Org2MSP.peer')"
   ```

2. Node.js 20+.

## Install and run (one command at a time)

From this `04-web-frontend` folder:

```bash
npm install
```

Point the app at your test-network checkout if it isn't a sibling of this folder:

```bash
export TEST_NETWORK_ROOT=/absolute/path/to/drunix/drunix-network/test-network
```

Start the server:

```bash
npm start
```

Open the UI:

```bash
open http://localhost:3000
```

## Using it

- **Create asset** — pick an ID, a value, and which org submits. Click create.
  Both ledger panels pulse and refresh; the sync line confirms they match.
- **Transfer ownership** — move an asset from one org to the other. Submit as the
  current owner.
- **Refresh both ledgers** — re-reads from each org's own peer and compares. A
  green "identical state" line means Org1's peer and Org2's peer independently
  hold the same data — the multi-org proof.

The log at the bottom shows each submit/commit and any errors verbatim.

## Environment variables

| Var | Default | Purpose |
|---|---|---|
| `TEST_NETWORK_ROOT` | `../../../drunix-network/test-network` | where the crypto material lives |
| `CHANNEL_NAME` | `mychannel` | channel |
| `CHAINCODE_NAME` | `registry` | chaincode name |
| `ORG1_PEER_ENDPOINT` | `localhost:7051` | Org1 lite peer |
| `ORG2_PEER_ENDPOINT` | `localhost:9051` | Org2 lite peer |
| `PORT` | `3000` | web server port |

---

## Honest caveats — read before relying on this

1. **Not run end-to-end by me.** I built this against the repo's verified sample
   code and compose facts, but I have no Go/Node runtime or live network here, so
   I could not execute it. Expect to debug paths/versions on first run.

2. **The two-org `AND` endorsement is the part most likely to need a tweak.**
   With `AND('Org1MSP.peer','Org2MSP.peer')`, a submit must gather endorsements
   from a peer in *each* org. The Fabric Gateway normally does this automatically
   via **service discovery** — the gateway peer asks the network which peers can
   satisfy the policy and collects their signatures. For that to work:
   - both orgs' peers must have **anchor peers** configured on the channel (the
     test-network's `createChannel` typically sets these), and
   - the gateway peer must be able to reach the other org's peer over gRPC.

   If a submit fails with an endorsement-policy or discovery error, the usual
   fixes are: confirm anchor peers are set (`./network.sh ... ` sets them in most
   builds), or fall back to the CLI flow in `03-multiorg-network/README.md` which
   names both `--peerAddresses` explicitly. I flag this because it's the one spot
   where a real two-org gateway run differs from the single-org sample, and I
   could not test it.

3. **Uses the Admin identity** of each org (matching the CLI demo). For anything
   beyond a local demo you'd enrol per-user identities via Fabric CA.

4. **No production hardening** — no auth on the HTTP endpoints, no rate limiting,
   no TLS on the browser↔server hop. It's a local demo tool. Don't expose it.

5. **`WhoAmI` / `GetAllAssets` / `ReadAsset` must exist in the deployed
   chaincode.** They do in the `03-multiorg-network` registry chaincode. If you
   point this at a different chaincode, adjust the function names in
   `server/app.js`.

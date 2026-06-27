/*
 * connection.js — builds a Fabric Gateway connection for one org.
 *
 * This is a near-verbatim adaptation of the connection logic in the Drunix repo's
 * asset-transfer-basic/application-gateway-javascript/src/app.js, refactored so a
 * connection can be created per-org (Org1 or Org2) on demand.
 *
 * SDK: @hyperledger/fabric-gateway ^1.10.0, @grpc/grpc-js ^1.14.0 (matches repo).
 */

const grpc = require('@grpc/grpc-js');
const { connect, hash, signers } = require('@hyperledger/fabric-gateway');
const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');

// Point this at your local Drunix test-network. Override with TEST_NETWORK_ROOT
// if your checkout lives elsewhere. Default assumes this app sits at
// drunix-network/<this-app> and the test-network is a sibling.
const TEST_NETWORK_ROOT = process.env.TEST_NETWORK_ROOT
    || path.resolve(__dirname, '..', '..', '..', 'drunix-network', 'test-network');

// Per-org connection parameters. Ports and host aliases come straight from the
// Drunix compose-test-net.yaml (LP for Org1 = 7051, LP for Org2 = 9051).
const ORG_CONFIG = {
    Org1MSP: {
        mspId: 'Org1MSP',
        peerEndpoint: process.env.ORG1_PEER_ENDPOINT || 'localhost:7051',
        peerHostAlias: 'peer0.org1.example.com',
        orgDomain: 'org1.example.com',
    },
    Org2MSP: {
        mspId: 'Org2MSP',
        peerEndpoint: process.env.ORG2_PEER_ENDPOINT || 'localhost:9051',
        peerHostAlias: 'peer0.org2.example.com',
        orgDomain: 'org2.example.com',
    },
};

function cryptoPathFor(orgDomain) {
    return path.resolve(
        TEST_NETWORK_ROOT,
        'organizations',
        'peerOrganizations',
        orgDomain
    );
}

async function getFirstDirFileName(dirPath) {
    const files = await fs.readdir(dirPath);
    const file = files[0];
    if (!file) {
        throw new Error(`No files in directory: ${dirPath}`);
    }
    return path.join(dirPath, file);
}

async function newGrpcConnection(cfg) {
    const cryptoPath = cryptoPathFor(cfg.orgDomain);
    const tlsCertPath = path.resolve(
        cryptoPath, 'peers', `peer0.${cfg.orgDomain}`, 'tls', 'ca.crt'
    );
    const tlsRootCert = await fs.readFile(tlsCertPath);
    const tlsCredentials = grpc.credentials.createSsl(tlsRootCert);
    return new grpc.Client(cfg.peerEndpoint, tlsCredentials, {
        'grpc.ssl_target_name_override': cfg.peerHostAlias,
    });
}

async function newIdentity(cfg) {
    const cryptoPath = cryptoPathFor(cfg.orgDomain);
    // Use the Admin user (matches the CLI demo, which acts as Admin@orgN).
    const certDir = path.resolve(
        cryptoPath, 'users', `Admin@${cfg.orgDomain}`, 'msp', 'signcerts'
    );
    const certPath = await getFirstDirFileName(certDir);
    const credentials = await fs.readFile(certPath);
    return { mspId: cfg.mspId, credentials };
}

async function newSigner(cfg) {
    const cryptoPath = cryptoPathFor(cfg.orgDomain);
    const keyDir = path.resolve(
        cryptoPath, 'users', `Admin@${cfg.orgDomain}`, 'msp', 'keystore'
    );
    const keyPath = await getFirstDirFileName(keyDir);
    const privateKeyPem = await fs.readFile(keyPath);
    const privateKey = crypto.createPrivateKey(privateKeyPem);
    return signers.newPrivateKeySigner(privateKey);
}

/**
 * Open a gateway + contract for the given org.
 * Returns { gateway, client, contract } — caller must close gateway & client.
 */
async function openContract(orgMspId, channelName, chaincodeName) {
    const cfg = ORG_CONFIG[orgMspId];
    if (!cfg) {
        throw new Error(`Unknown org: ${orgMspId}. Use Org1MSP or Org2MSP.`);
    }

    const client = await newGrpcConnection(cfg);
    const gateway = connect({
        client,
        identity: await newIdentity(cfg),
        signer: await newSigner(cfg),
        hash: hash.sha256,
        evaluateOptions: () => ({ deadline: Date.now() + 5000 }),
        endorseOptions: () => ({ deadline: Date.now() + 15000 }),
        submitOptions: () => ({ deadline: Date.now() + 5000 }),
        commitStatusOptions: () => ({ deadline: Date.now() + 60000 }),
    });

    const network = gateway.getNetwork(channelName);
    const contract = network.getContract(chaincodeName);
    return { gateway, client, contract };
}

module.exports = { openContract, ORG_CONFIG, TEST_NETWORK_ROOT };

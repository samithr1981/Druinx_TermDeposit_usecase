/*
 * app.js — Express server that drives the Drunix two-org network.
 *
 * Endpoints:
 *   POST /api/create   { id, value, org }        -> CreateAsset (submit)
 *   POST /api/transfer { id, newOwnerOrg, org }  -> TransferAsset (submit)
 *   GET  /api/asset/:id?org=Org1MSP              -> ReadAsset (evaluate)
 *   GET  /api/assets?org=Org1MSP                 -> GetAllAssets (evaluate)
 *   GET  /api/whoami?org=Org1MSP                 -> WhoAmI (evaluate)
 *
 * 'org' selects which org's peer/identity submits the request. The chaincode's
 * AND('Org1MSP.peer','Org2MSP.peer') endorsement policy still requires BOTH orgs
 * to endorse — that gathering is done by the gateway via service discovery (see
 * README note on enabling discovery / the two-org endorsement caveat).
 */

const express = require('express');
const path = require('node:path');
const { TextDecoder } = require('node:util');
const { openContract } = require('./connection');

const app = express();
app.use(express.json());
app.use(express.static(path.join(__dirname, '..', 'public')));

const CHANNEL = process.env.CHANNEL_NAME || 'mychannel';
const CHAINCODE = process.env.CHAINCODE_NAME || 'registry';
const PORT = process.env.PORT || 3000;
const utf8 = new TextDecoder();

// helper: run a function with a freshly opened contract, always closing it
async function withContract(orgMspId, fn) {
    const { gateway, client, contract } = await openContract(
        orgMspId, CHANNEL, CHAINCODE
    );
    try {
        return await fn(contract);
    } finally {
        gateway.close();
        client.close();
    }
}

function pickOrg(value) {
    const org = value || 'Org1MSP';
    if (org !== 'Org1MSP' && org !== 'Org2MSP') {
        throw new Error("org must be 'Org1MSP' or 'Org2MSP'");
    }
    return org;
}

// ---- CreateAsset (submit / write) ----
app.post('/api/create', async (req, res) => {
    try {
        const { id, value } = req.body;
        const org = pickOrg(req.body.org);
        if (!id || value === undefined) {
            return res.status(400).json({ error: 'id and value are required' });
        }
        await withContract(org, (contract) =>
            contract.submitTransaction('CreateAsset', String(id), String(value))
        );
        res.json({ ok: true, message: `Asset ${id} created by ${org}`, org });
    } catch (err) {
        res.status(500).json({ error: String(err.message || err) });
    }
});

// ---- TransferAsset (submit / write) ----
app.post('/api/transfer', async (req, res) => {
    try {
        const { id, newOwnerOrg } = req.body;
        const org = pickOrg(req.body.org);
        if (!id || !newOwnerOrg) {
            return res.status(400).json({ error: 'id and newOwnerOrg are required' });
        }
        await withContract(org, (contract) =>
            contract.submitTransaction('TransferAsset', String(id), String(newOwnerOrg))
        );
        res.json({ ok: true, message: `Asset ${id} transferred to ${newOwnerOrg}`, org });
    } catch (err) {
        res.status(500).json({ error: String(err.message || err) });
    }
});

// ---- ReadAsset (evaluate / query) ----
app.get('/api/asset/:id', async (req, res) => {
    try {
        const org = pickOrg(req.query.org);
        const bytes = await withContract(org, (contract) =>
            contract.evaluateTransaction('ReadAsset', req.params.id)
        );
        res.json({ ok: true, queriedFrom: org, asset: JSON.parse(utf8.decode(bytes)) });
    } catch (err) {
        res.status(500).json({ error: String(err.message || err) });
    }
});

// ---- GetAllAssets (evaluate / query) ----
app.get('/api/assets', async (req, res) => {
    try {
        const org = pickOrg(req.query.org);
        const bytes = await withContract(org, (contract) =>
            contract.evaluateTransaction('GetAllAssets')
        );
        const text = utf8.decode(bytes);
        res.json({ ok: true, queriedFrom: org, assets: text ? JSON.parse(text) : [] });
    } catch (err) {
        res.status(500).json({ error: String(err.message || err) });
    }
});

// ---- WhoAmI (evaluate / query) ----
app.get('/api/whoami', async (req, res) => {
    try {
        const org = pickOrg(req.query.org);
        const bytes = await withContract(org, (contract) =>
            contract.evaluateTransaction('WhoAmI')
        );
        res.json({ ok: true, queriedFrom: org, whoami: utf8.decode(bytes) });
    } catch (err) {
        res.status(500).json({ error: String(err.message || err) });
    }
});

app.listen(PORT, () => {
    console.log(`Drunix multi-org front end on http://localhost:${PORT}`);
    console.log(`  channel=${CHANNEL} chaincode=${CHAINCODE}`);
    console.log('  Make sure the Drunix test-network is up and the chaincode is deployed.');
});

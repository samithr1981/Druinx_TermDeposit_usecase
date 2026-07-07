package chaincode

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// =============================================================================
// ABC Bank — Fixed Deposit Tokenisation chaincode (Drunix / Hyperledger Fabric)
//
// Functional port of the Chia SpendSim demo (abcbank_fd_demo.py).
//
// NOTE ON THE PORT
// ----------------
// Chia is UTXO + smart-coin (CLVM). Rules (timelock, clawback) are enforced
// cryptographically inside each coin's puzzle. Drunix/Fabric is a permissioned
// key-value ledger: there are no "coins". Tokens here are ledger records and
// the rules are enforced by (a) chaincode logic, (b) MSP client identity, and
// (c) the channel endorsement policy.
//
// Mapping of concepts:
//   Chia CAT token            -> FDToken ledger record (this contract)
//   CAT TAIL hash             -> token symbol / InstrumentID field
//   bank BLS key / clawback   -> MSP identity check (only BankMSP may clawback)
//   depositor BLS sig         -> MSP identity check (only the depositor's org/id)
//   ASSERT_HEIGHT_ABSOLUTE    -> MaturityBlock compared to ctx.GetStub block height
//   SpendSim farm_block       -> Fabric block commits (or a simulated counter)
//   Chia DataLayer            -> a second world-state record (FDRecord)
//
// IMPORTANT: block height in Fabric is NOT directly readable inside chaincode
// the way it is in Chia. Fabric chaincode is deterministic and cannot read the
// committing block height at endorsement time. We therefore model maturity with
// the transaction timestamp (ctx.GetStub().GetTxTimestamp()), which IS
// deterministic and available. Block-height timelock semantics are emulated via
// a maturity *timestamp*. This is the standard Fabric pattern and is called out
// explicitly so reviewers understand the deviation from the Chia version.
// =============================================================================

type SmartContract struct {
	contractapi.Contract
}

// FDToken is the tokenised fixed deposit (the "CAT" equivalent).
// Fields in alphabetical order for cross-language determinism.
type FDToken struct {
	Amount         uint64 `json:"Amount"`         // token units (= INR principal, 1 token = INR 1)
	ClawbackReason string `json:"ClawbackReason"` // populated when status is CLAWBACK_INITIATED
	DepositTime    int64  `json:"DepositTime"`    // unix seconds, from tx timestamp
	DepositorDID   string `json:"DepositorDID"`
	DepositorID    string `json:"DepositorID"` // MSP client identity that may redeem
	DocType        string `json:"DocType"`     // "fdtoken"
	ID             string `json:"ID"`          // e.g. ABCFD-001
	InterestUnits  uint64 `json:"InterestUnits"`
	MaturityTime   int64  `json:"MaturityTime"` // unix seconds; redemption blocked before this
	MaturityUnits  uint64 `json:"MaturityUnits"`
	Owner          string `json:"Owner"`  // current holder MSP id (depositor or bank after clawback)
	Status         string `json:"Status"` // ACTIVE | CLAWBACK_INITIATED | REDEEMED
	Symbol         string `json:"Symbol"` // token symbol, analogous to CAT TAIL identity
}

// FDRecord is the off-token KYC / audit data (the "DataLayer" equivalent).
type FDRecord struct {
	DepositorDID  string            `json:"DepositorDID"`
	DepositorName string            `json:"DepositorName"`
	DepositorPAN  string            `json:"DepositorPAN"`
	DocType       string            `json:"DocType"` // "fdrecord"
	ID            string            `json:"ID"`      // same key as the FDToken ID
	KYCStatus     string            `json:"KYCStatus"`
	Meta          map[string]string `json:"Meta"` // free-form, mirrors Chia DataLayer key/values
	NomineeDID    string            `json:"NomineeDID"`
	NomineeName   string            `json:"NomineeName"`
}

// constants matching the Chia demo economics
const (
	BankMSPID   = "Org1MSP" // ABC Bank in the test-network
	statusActive   = "ACTIVE"
	statusClawback = "CLAWBACK_INITIATED"
	statusRedeemed = "REDEEMED"
	docTypeToken   = "fdtoken"
	docTypeRecord  = "fdrecord"
)

// ---------------------------------------------------------------------------
// helper: read the invoking client's MSP id
// ---------------------------------------------------------------------------
func clientMSPID(ctx contractapi.TransactionContextInterface) (string, error) {
	mspID, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return "", fmt.Errorf("failed to get client MSP id: %v", err)
	}
	return mspID, nil
}

func clientID(ctx contractapi.TransactionContextInterface) (string, error) {
	id, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return "", fmt.Errorf("failed to get client id: %v", err)
	}
	return id, nil
}

func txTime(ctx contractapi.TransactionContextInterface) (int64, error) {
	ts, err := ctx.GetStub().GetTxTimestamp()
	if err != nil {
		return 0, fmt.Errorf("failed to read tx timestamp: %v", err)
	}
	return ts.Seconds, nil
}

// ===========================================================================
// PHASE 1 — KYC & DID  (writes the FDRecord / DataLayer)
// ===========================================================================

// RegisterKYC records depositor + nominee identity. Bank-only operation.
func (s *SmartContract) RegisterKYC(
	ctx contractapi.TransactionContextInterface,
	id string,
	depositorName string,
	depositorPAN string,
	depositorDID string,
	nomineeName string,
	nomineeDID string,
) error {
	msp, err := clientMSPID(ctx)
	if err != nil {
		return err
	}
	if msp != BankMSPID {
		return fmt.Errorf("only %s (ABC Bank) may register KYC, caller is %s", BankMSPID, msp)
	}

	rec := FDRecord{
		ID:            id,
		DocType:       docTypeRecord,
		DepositorName: depositorName,
		DepositorPAN:  depositorPAN,
		DepositorDID:  depositorDID,
		KYCStatus:     "VERIFIED",
		NomineeName:   nomineeName,
		NomineeDID:    nomineeDID,
		Meta:          map[string]string{},
	}
	recJSON, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(recordKey(id), recJSON)
}

// ===========================================================================
// PHASE 2 — DEPOSIT & TOKEN MINTING
// ===========================================================================

// MintFD issues a new tokenised FD. Bank-only. depositorID is the MSP client
// identity string (GetID()) of the wallet allowed to redeem at maturity.
// tenorSeconds emulates the Chia TENOR_BLOCKS timelock.
func (s *SmartContract) MintFD(
	ctx contractapi.TransactionContextInterface,
	id string,
	symbol string,
	principalUnits uint64,
	interestUnits uint64,
	tenorSeconds int64,
	depositorID string,
	depositorDID string,
) error {
	msp, err := clientMSPID(ctx)
	if err != nil {
		return err
	}
	if msp != BankMSPID {
		return fmt.Errorf("only %s (ABC Bank) may mint, caller is %s", BankMSPID, msp)
	}

	exists, err := s.FDExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("FD %s already exists", id)
	}

	now, err := txTime(ctx)
	if err != nil {
		return err
	}

	token := FDToken{
		ID:            id,
		DocType:       docTypeToken,
		Symbol:        symbol,
		Amount:        principalUnits,
		InterestUnits: interestUnits,
		MaturityUnits: principalUnits + interestUnits,
		DepositTime:   now,
		MaturityTime:  now + tenorSeconds,
		DepositorID:   depositorID,
		DepositorDID:  depositorDID,
		Owner:         depositorID,
		Status:        statusActive,
	}
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return err
	}
	if err := ctx.GetStub().PutState(tokenKey(id), tokenJSON); err != nil {
		return fmt.Errorf("failed to write token: %v", err)
	}

	// emit an event for off-chain listeners (mirrors the audit log)
	_ = ctx.GetStub().SetEvent("FDMinted", tokenJSON)
	return nil
}

// ===========================================================================
// PHASE 3 — TIMELOCK: EARLY REDEMPTION ATTEMPT (enforced inside RedeemFD)
//   There is no separate function: the timelock is a guard in RedeemFD.
//   A read-only helper lets callers check remaining time.
// ===========================================================================

// TimeToMaturity returns seconds remaining until maturity (negative if matured).
func (s *SmartContract) TimeToMaturity(ctx contractapi.TransactionContextInterface, id string) (int64, error) {
	token, err := s.ReadFD(ctx, id)
	if err != nil {
		return 0, err
	}
	now, err := txTime(ctx)
	if err != nil {
		return 0, err
	}
	return token.MaturityTime - now, nil
}

// ===========================================================================
// PHASE 4 — CLAWBACK (AML regulatory recall). Bank-only.
//   Chia: bank BLS signature recalls the coin.
//   Drunix: only BankMSP identity may flip status + reassign Owner to bank.
// ===========================================================================

func (s *SmartContract) ClawbackFD(
	ctx contractapi.TransactionContextInterface,
	id string,
	reason string,
) error {
	msp, err := clientMSPID(ctx)
	if err != nil {
		return err
	}
	if msp != BankMSPID {
		return fmt.Errorf("only %s (ABC Bank) may clawback, caller is %s", BankMSPID, msp)
	}

	token, err := s.ReadFD(ctx, id)
	if err != nil {
		return err
	}
	if token.Status == statusRedeemed {
		return fmt.Errorf("FD %s already redeemed, cannot clawback", id)
	}

	bankID, err := clientID(ctx)
	if err != nil {
		return err
	}

	token.Status = statusClawback
	token.ClawbackReason = reason
	token.Owner = bankID // recalled to the bank

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return err
	}
	if err := ctx.GetStub().PutState(tokenKey(id), tokenJSON); err != nil {
		return err
	}
	_ = ctx.GetStub().SetEvent("FDClawback", tokenJSON)
	return nil
}

// RestoreFD clears a clawback (AML cleared) and returns the token to the
// depositor. Bank-only.
func (s *SmartContract) RestoreFD(ctx contractapi.TransactionContextInterface, id string) error {
	msp, err := clientMSPID(ctx)
	if err != nil {
		return err
	}
	if msp != BankMSPID {
		return fmt.Errorf("only %s (ABC Bank) may restore, caller is %s", BankMSPID, msp)
	}

	token, err := s.ReadFD(ctx, id)
	if err != nil {
		return err
	}
	if token.Status != statusClawback {
		return fmt.Errorf("FD %s is not in clawback state (status=%s)", id, token.Status)
	}

	token.Status = statusActive
	token.ClawbackReason = ""
	token.Owner = token.DepositorID

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(tokenKey(id), tokenJSON)
}

// ===========================================================================
// PHASE 6 — MATURITY REDEMPTION (burn token, mark REDEEMED)
//   Guards: (1) caller identity == depositor  (Chia: depositor BLS sig)
//           (2) tx time >= MaturityTime        (Chia: ASSERT_HEIGHT_ABSOLUTE)
//           (3) status == ACTIVE
// ===========================================================================

func (s *SmartContract) RedeemFD(ctx contractapi.TransactionContextInterface, id string) (string, error) {
	token, err := s.ReadFD(ctx, id)
	if err != nil {
		return "", err
	}

	callerID, err := clientID(ctx)
	if err != nil {
		return "", err
	}
	if callerID != token.DepositorID {
		return "", fmt.Errorf("redemption denied: caller is not the depositor of FD %s", id)
	}

	if token.Status != statusActive {
		return "", fmt.Errorf("FD %s not redeemable, status=%s", id, token.Status)
	}

	now, err := txTime(ctx)
	if err != nil {
		return "", err
	}
	if now < token.MaturityTime {
		return "", fmt.Errorf("timelock active: FD %s matures at %d, current tx time %d (%d seconds remaining)",
			id, token.MaturityTime, now, token.MaturityTime-now)
	}

	// "burn": mark redeemed and zero the balance
	payout := token.MaturityUnits
	token.Status = statusRedeemed
	token.Amount = 0

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	if err := ctx.GetStub().PutState(tokenKey(id), tokenJSON); err != nil {
		return "", err
	}
	_ = ctx.GetStub().SetEvent("FDRedeemed", tokenJSON)

	return fmt.Sprintf("REDEEMED:%s:payout_units=%d", id, payout), nil
}

// ===========================================================================
// DataLayer-style free-form metadata (Chia dl.set)
// ===========================================================================

func (s *SmartContract) SetMeta(ctx contractapi.TransactionContextInterface, id, key, value string) error {
	msp, err := clientMSPID(ctx)
	if err != nil {
		return err
	}
	if msp != BankMSPID {
		return fmt.Errorf("only %s may write metadata", BankMSPID)
	}
	recJSON, err := ctx.GetStub().GetState(recordKey(id))
	if err != nil {
		return err
	}
	if recJSON == nil {
		return fmt.Errorf("no FDRecord for %s; call RegisterKYC first", id)
	}
	var rec FDRecord
	if err := json.Unmarshal(recJSON, &rec); err != nil {
		return err
	}
	if rec.Meta == nil {
		rec.Meta = map[string]string{}
	}
	rec.Meta[key] = value
	out, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(recordKey(id), out)
}

// WhoAmI returns the caller identity's MSP client ID -- useful for discovering
// the correct depositorID string to pass into MintFD before a real enrolment
// workflow exists.
func (s *SmartContract) WhoAmI(ctx contractapi.TransactionContextInterface) (string, error) {
	return clientID(ctx)
}

// ===========================================================================
// READ HELPERS
// ===========================================================================

func (s *SmartContract) ReadFD(ctx contractapi.TransactionContextInterface, id string) (*FDToken, error) {
	tokenJSON, err := ctx.GetStub().GetState(tokenKey(id))
	if err != nil {
		return nil, fmt.Errorf("failed to read FD: %v", err)
	}
	if tokenJSON == nil {
		return nil, fmt.Errorf("FD %s does not exist", id)
	}
	var token FDToken
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *SmartContract) ReadRecord(ctx contractapi.TransactionContextInterface, id string) (*FDRecord, error) {
	recJSON, err := ctx.GetStub().GetState(recordKey(id))
	if err != nil {
		return nil, fmt.Errorf("failed to read FDRecord: %v", err)
	}
	if recJSON == nil {
		return nil, fmt.Errorf("FDRecord %s does not exist", id)
	}
	var rec FDRecord
	if err := json.Unmarshal(recJSON, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *SmartContract) FDExists(ctx contractapi.TransactionContextInterface, id string) (bool, error) {
	tokenJSON, err := ctx.GetStub().GetState(tokenKey(id))
	if err != nil {
		return false, fmt.Errorf("failed to read FD: %v", err)
	}
	return tokenJSON != nil, nil
}

// GetAllFDs returns every FDToken on the ledger.
func (s *SmartContract) GetAllFDs(ctx contractapi.TransactionContextInterface) ([]*FDToken, error) {
	it, err := ctx.GetStub().GetStateByRange("", "")
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var tokens []*FDToken
	for it.HasNext() {
		kv, err := it.Next()
		if err != nil {
			return nil, err
		}
		var token FDToken
		if err := json.Unmarshal(kv.Value, &token); err != nil {
			continue // skip non-token records (e.g. FDRecord)
		}
		if token.DocType == docTypeToken {
			tokens = append(tokens, &token)
		}
	}
	return tokens, nil
}

// ---------------------------------------------------------------------------
// key helpers — keep token and record records in separate namespaces
// ---------------------------------------------------------------------------
func tokenKey(id string) string  { return "TOKEN_" + id }
func recordKey(id string) string { return "RECORD_" + id }

// small util used by the maturity emulation when wiring from a CLI that passes
// tenor as a string of days
func DaysToSeconds(days string) (int64, error) {
	d, err := strconv.ParseInt(days, 10, 64)
	if err != nil {
		return 0, err
	}
	return d * 24 * 60 * 60, nil
}

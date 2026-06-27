package chaincode

import (
	"encoding/json"
	"fmt"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// =============================================================================
// Drunix P2P Token Transfer — demo chaincode
//
// A minimal person-to-person value-transfer ledger on Drunix (the NPCI fork of
// Hyperledger Fabric). Each participant has an Account holding an INR-denominated
// token balance. Holders transfer value directly to one another. All rules are
// enforced by this chaincode plus the caller's MSP identity.
//
// WHY THIS SHAPE (vs a Chia/UTXO design)
// --------------------------------------
// Drunix is an account/balance, key-value ledger. A "wallet" here is an Account
// record: { ID, Owner, Balance }. A transfer debits one record and credits
// another in the SAME transaction, so it is atomic — both updates commit or
// neither does. There are no coins to split or merge; we just move a number.
//
// IDENTITY MODEL
// --------------
//   - Each Account stores Owner = the MSP client ID (GetID()) allowed to spend it.
//   - Transfer is authorised only if the caller's client ID == sender's Owner.
//   - A designated minter org (MinterMSPID) may create new tokens (Mint).
// =============================================================================

type SmartContract struct {
	contractapi.Contract
}

// Account is a P2P wallet: one balance owned by one identity.
// Fields alphabetical for cross-language determinism.
type Account struct {
	Balance  uint64 `json:"Balance"`  // token units; 1 token = INR 1 in this demo
	DocType  string `json:"DocType"`  // "account"
	ID       string `json:"ID"`       // human handle, e.g. "ramesh" or "sunita"
	Owner    string `json:"Owner"`    // MSP client identity permitted to spend
	OwnerOrg string `json:"OwnerOrg"` // MSP id (org) of the owner, for display
}

// Transfer is an immutable receipt written for every successful transfer,
// giving an on-ledger audit trail (queryable by GetAllTransfers).
type Transfer struct {
	Amount   uint64 `json:"Amount"`
	DocType  string `json:"DocType"` // "transfer"
	From     string `json:"From"`
	ID       string `json:"ID"` // the tx id, unique
	To       string `json:"To"`
	When     int64  `json:"When"` // unix seconds (tx timestamp)
}

const (
	// In the test-network, Org1 acts as the token issuer / central minter.
	MinterMSPID = "Org1MSP"

	docTypeAccount  = "account"
	docTypeTransfer = "transfer"
)

// ---------------------------------------------------------------------------
// identity + time helpers
// ---------------------------------------------------------------------------
func callerMSPID(ctx contractapi.TransactionContextInterface) (string, error) {
	id, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return "", fmt.Errorf("failed to get caller MSP id: %v", err)
	}
	return id, nil
}

func callerID(ctx contractapi.TransactionContextInterface) (string, error) {
	id, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return "", fmt.Errorf("failed to get caller id: %v", err)
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

func accountKey(id string) string  { return "ACCT_" + id }
func transferKey(id string) string { return "XFER_" + id }

// ===========================================================================
// OpenAccount — create a wallet. The caller becomes the Owner.
// Anyone may open their own account.
// ===========================================================================
func (s *SmartContract) OpenAccount(ctx contractapi.TransactionContextInterface, id string) error {
	exists, err := s.AccountExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("account %s already exists", id)
	}

	owner, err := callerID(ctx)
	if err != nil {
		return err
	}
	org, err := callerMSPID(ctx)
	if err != nil {
		return err
	}

	acct := Account{
		ID:       id,
		DocType:  docTypeAccount,
		Owner:    owner,
		OwnerOrg: org,
		Balance:  0,
	}
	acctJSON, err := json.Marshal(acct)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(accountKey(id), acctJSON)
}

// ===========================================================================
// Mint — issue new tokens into an account. Minter-org only.
// (Models the issuer/treasury topping up a wallet.)
// ===========================================================================
func (s *SmartContract) Mint(ctx contractapi.TransactionContextInterface, id string, amount uint64) error {
	org, err := callerMSPID(ctx)
	if err != nil {
		return err
	}
	if org != MinterMSPID {
		return fmt.Errorf("only %s may mint, caller org is %s", MinterMSPID, org)
	}
	if amount == 0 {
		return fmt.Errorf("mint amount must be greater than zero")
	}

	acct, err := s.ReadAccount(ctx, id)
	if err != nil {
		return err
	}
	acct.Balance += amount

	acctJSON, err := json.Marshal(acct)
	if err != nil {
		return err
	}
	if err := ctx.GetStub().PutState(accountKey(id), acctJSON); err != nil {
		return err
	}
	_ = ctx.GetStub().SetEvent("Mint", acctJSON)
	return nil
}

// ===========================================================================
// Transfer — the core P2P operation. Atomically debits 'from' and credits 'to'.
// Authorisation: caller's client ID must equal the 'from' account's Owner.
// ===========================================================================
func (s *SmartContract) Transfer(
	ctx contractapi.TransactionContextInterface,
	from string,
	to string,
	amount uint64,
) (string, error) {
	if from == to {
		return "", fmt.Errorf("cannot transfer to the same account")
	}
	if amount == 0 {
		return "", fmt.Errorf("transfer amount must be greater than zero")
	}

	sender, err := s.ReadAccount(ctx, from)
	if err != nil {
		return "", err
	}
	receiver, err := s.ReadAccount(ctx, to)
	if err != nil {
		return "", err
	}

	// authorisation: only the owner of 'from' may move its funds
	caller, err := callerID(ctx)
	if err != nil {
		return "", err
	}
	if caller != sender.Owner {
		return "", fmt.Errorf("transfer denied: caller is not the owner of account %s", from)
	}

	if sender.Balance < amount {
		return "", fmt.Errorf("insufficient balance in %s: have %d, need %d", from, sender.Balance, amount)
	}

	// atomic debit + credit (both writes are in one transaction)
	sender.Balance -= amount
	receiver.Balance += amount

	senderJSON, err := json.Marshal(sender)
	if err != nil {
		return "", err
	}
	receiverJSON, err := json.Marshal(receiver)
	if err != nil {
		return "", err
	}
	if err := ctx.GetStub().PutState(accountKey(from), senderJSON); err != nil {
		return "", err
	}
	if err := ctx.GetStub().PutState(accountKey(to), receiverJSON); err != nil {
		return "", err
	}

	// write an immutable transfer receipt keyed by the Fabric tx id
	now, err := txTime(ctx)
	if err != nil {
		return "", err
	}
	txID := ctx.GetStub().GetTxID()
	receipt := Transfer{
		ID:      txID,
		DocType: docTypeTransfer,
		From:    from,
		To:      to,
		Amount:  amount,
		When:    now,
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	if err := ctx.GetStub().PutState(transferKey(txID), receiptJSON); err != nil {
		return "", err
	}
	_ = ctx.GetStub().SetEvent("Transfer", receiptJSON)

	return fmt.Sprintf("OK:%s sent %d to %s (txid=%s)", from, amount, to, txID), nil
}

// ===========================================================================
// READ / QUERY helpers
// ===========================================================================

func (s *SmartContract) ReadAccount(ctx contractapi.TransactionContextInterface, id string) (*Account, error) {
	acctJSON, err := ctx.GetStub().GetState(accountKey(id))
	if err != nil {
		return nil, fmt.Errorf("failed to read account: %v", err)
	}
	if acctJSON == nil {
		return nil, fmt.Errorf("account %s does not exist", id)
	}
	var acct Account
	if err := json.Unmarshal(acctJSON, &acct); err != nil {
		return nil, err
	}
	return &acct, nil
}

func (s *SmartContract) AccountExists(ctx contractapi.TransactionContextInterface, id string) (bool, error) {
	acctJSON, err := ctx.GetStub().GetState(accountKey(id))
	if err != nil {
		return false, fmt.Errorf("failed to read account: %v", err)
	}
	return acctJSON != nil, nil
}

func (s *SmartContract) BalanceOf(ctx contractapi.TransactionContextInterface, id string) (uint64, error) {
	acct, err := s.ReadAccount(ctx, id)
	if err != nil {
		return 0, err
	}
	return acct.Balance, nil
}

// GetAllAccounts returns every account on the ledger.
func (s *SmartContract) GetAllAccounts(ctx contractapi.TransactionContextInterface) ([]*Account, error) {
	it, err := ctx.GetStub().GetStateByRange("ACCT_", "ACCT_~")
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var accounts []*Account
	for it.HasNext() {
		kv, err := it.Next()
		if err != nil {
			return nil, err
		}
		var acct Account
		if err := json.Unmarshal(kv.Value, &acct); err != nil {
			continue
		}
		if acct.DocType == docTypeAccount {
			accounts = append(accounts, &acct)
		}
	}
	return accounts, nil
}

// GetAllTransfers returns the full transfer history (audit trail).
func (s *SmartContract) GetAllTransfers(ctx contractapi.TransactionContextInterface) ([]*Transfer, error) {
	it, err := ctx.GetStub().GetStateByRange("XFER_", "XFER_~")
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var transfers []*Transfer
	for it.HasNext() {
		kv, err := it.Next()
		if err != nil {
			return nil, err
		}
		var t Transfer
		if err := json.Unmarshal(kv.Value, &t); err != nil {
			continue
		}
		if t.DocType == docTypeTransfer {
			transfers = append(transfers, &t)
		}
	}
	return transfers, nil
}

// WhoAmI returns the caller's MSP client ID — useful when wiring up the
// Owner field during a demo (so you know what string to expect).
func (s *SmartContract) WhoAmI(ctx contractapi.TransactionContextInterface) (string, error) {
	return callerID(ctx)
}

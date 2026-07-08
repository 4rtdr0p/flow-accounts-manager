package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/datastore"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
)

func testUserPublicKey(t *testing.T) (string, crypto.PublicKey) {
	t.Helper()

	seed := make([]byte, 64)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}

	privateKey, err := crypto.GeneratePrivateKey(crypto.ECDSA_P256, seed)
	if err != nil {
		t.Fatal(err)
	}

	return privateKey.PublicKey().String(), privateKey.PublicKey()
}

func TestGraduateToSelfCustodyRetriesOnTransientSaveFailure(t *testing.T) {
	userPublicKeyString, userPublicKey := testUserPublicKey(t)

	store := &graduateStore{
		account: Account{
			Address: "0xf8d6e0586b0a20c7",
			Type:    AccountTypeCustodial,
			Keys: []keys.Storable{
				{Index: 0, PublicKey: "0x01", SignAlgo: "ECDSA_P256"},
				{Index: 1, PublicKey: "0x02", SignAlgo: "ECDSA_P256"},
			},
		},
		saveErr:          &testTransientError{msg: "db connection reset"},
		saveSuccessAfter: 3,
	}
	txs := &graduateTxService{}
	svc := &ServiceImpl{
		cfg:   &configs.Config{ChainID: flow.Emulator},
		store: store,
		fc:    newGraduateFlowClient(userPublicKey, []uint32{0, 1}),
		txs:   txs,
	}

	start := time.Now()
	account, err := svc.GraduateToSelfCustody(context.Background(), store.account.Address, userPublicKeyString)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected graduation to succeed after retries, got %v", err)
	}

	if account.Type != AccountTypeNonCustodial {
		t.Fatalf("expected account type %q, got %q", AccountTypeNonCustodial, account.Type)
	}

	if store.saveAttempts != 3 {
		t.Fatalf("expected 3 save attempts (initial + 2 retries), got %d", store.saveAttempts)
	}

	// Backoffs are 100ms + 500ms + 1s = ~1.6s minimum for three retries.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("expected retry backoff to delay return, elapsed %v", elapsed)
	}

	if len(txs.createCodes) != 1 {
		t.Fatalf("expected 1 graduation tx submission, got %d", len(txs.createCodes))
	}
	if !strings.Contains(txs.createCodes[0], "transaction(userPublicKey: String, revokeKeyIndices: [Int])") {
		t.Fatalf("expected graduation tx, got %q", txs.createCodes[0])
	}
}

func TestGraduateToSelfCustodyFailsOnNonTransientSaveError(t *testing.T) {
	userPublicKeyString, userPublicKey := testUserPublicKey(t)

	store := &graduateStore{
		account: Account{
			Address: "0xf8d6e0586b0a20c7",
			Type:    AccountTypeCustodial,
			Keys: []keys.Storable{
				{Index: 0, PublicKey: "0x01", SignAlgo: "ECDSA_P256"},
			},
		},
		saveErr: errors.New("unique constraint violation: duplicate key"),
	}
	txs := &graduateTxService{}
	svc := &ServiceImpl{
		cfg:   &configs.Config{ChainID: flow.Emulator},
		store: store,
		fc:    newGraduateFlowClient(userPublicKey, []uint32{0}),
		txs:   txs,
	}

	start := time.Now()
	_, err := svc.GraduateToSelfCustody(context.Background(), store.account.Address, userPublicKeyString)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "unique constraint violation") {
		t.Fatalf("expected non-transient save error, got %v", err)
	}

	if store.saveAttempts != 1 {
		t.Fatalf("expected no retries for non-transient error, got %d save attempts", store.saveAttempts)
	}

	if elapsed > 50*time.Millisecond {
		t.Fatalf("expected immediate failure without backoff, elapsed %v", elapsed)
	}
}

func TestReconcileAccountUpdatesCustodialToNonCustodial(t *testing.T) {
	_, userPublicKey := testUserPublicKey(t)

	store := &graduateStore{
		account: Account{
			Address: "0xf8d6e0586b0a20c7",
			Type:    AccountTypeCustodial,
			Keys: []keys.Storable{
				{Index: 0, PublicKey: "0x01", SignAlgo: "ECDSA_P256"},
				{Index: 1, PublicKey: "0x02", SignAlgo: "ECDSA_P256"},
			},
		},
	}
	svc := &ServiceImpl{
		cfg:   &configs.Config{ChainID: flow.Emulator},
		store: store,
		fc:    newGraduateFlowClient(userPublicKey, []uint32{0, 1}),
	}

	account, err := svc.ReconcileAccountWithChain(context.Background(), store.account.Address)
	if err != nil {
		t.Fatalf("expected reconcile to succeed, got %v", err)
	}

	if account.Type != AccountTypeNonCustodial {
		t.Fatalf("expected reconciled account type %q, got %q", AccountTypeNonCustodial, account.Type)
	}

	if store.saved.Type != AccountTypeNonCustodial {
		t.Fatalf("expected database account to be updated to non-custodial, got %q", store.saved.Type)
	}
}

func TestReconcileAccountSkipsNonCustodial(t *testing.T) {
	store := &graduateStore{
		account: Account{
			Address: "0xf8d6e0586b0a20c7",
			Type:    AccountTypeNonCustodial,
			Keys: []keys.Storable{
				{Index: 0, PublicKey: "0x01", SignAlgo: "ECDSA_P256"},
			},
		},
	}
	// On-chain state still shows active custodial keys, but DB is already non-custodial.
	svc := &ServiceImpl{
		cfg:   &configs.Config{ChainID: flow.Emulator},
		store: store,
		fc:    newGraduateFlowClient(nil, []uint32{}),
	}

	account, err := svc.ReconcileAccountWithChain(context.Background(), store.account.Address)
	if err != nil {
		t.Fatalf("expected reconcile to succeed, got %v", err)
	}

	if account.Type != AccountTypeNonCustodial {
		t.Fatalf("expected account to remain non-custodial, got %q", account.Type)
	}

	if store.saveAttempts != 0 {
		t.Fatalf("expected no database update for already non-custodial account, got %d saves", store.saveAttempts)
	}
}

type graduateStore struct {
	account          Account
	saved            Account
	saveErr          error
	saveSuccessAfter int
	saveAttempts     int
}

func (s *graduateStore) Accounts(datastore.ListOptions) ([]Account, error) { return nil, nil }

func (s *graduateStore) Account(address string) (Account, error) {
	return s.account, nil
}

func (s *graduateStore) InsertAccount(a *Account) error { return nil }

func (s *graduateStore) SaveAccount(a *Account) error {
	s.saveAttempts++
	s.saved = *a
	if s.saveErr == nil {
		return nil
	}
	if s.saveSuccessAfter > 0 && s.saveAttempts >= s.saveSuccessAfter {
		return nil
	}
	return s.saveErr
}

func (s *graduateStore) InsertKey(k *keys.Storable) error { panic("not used") }

func (s *graduateStore) ArchiveKey(id int) error { panic("not used") }

func (s *graduateStore) RotateKeyState(oldKeyID int, newKey *keys.Storable) error {
	panic("not used")
}

func (s *graduateStore) HardDeleteAccount(a *Account) error { return nil }

type graduateTxService struct {
	createCodes []string
}

func (s *graduateTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	s.createCodes = append(s.createCodes, code)
	return nil, nil, nil
}

func (s *graduateTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	return &transactions.SignedTransaction{}, nil
}

func (s *graduateTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}

func (s *graduateTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}

func (s *graduateTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}

func (s *graduateTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}

func (s *graduateTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	panic("not used")
}

func (s *graduateTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used")
}

func (s *graduateTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used")
}

type graduateFlowClient struct {
	userPublicKey     crypto.PublicKey
	revokedKeyIndices []uint32
}

func newGraduateFlowClient(userPublicKey crypto.PublicKey, revokedKeyIndices []uint32) *graduateFlowClient {
	return &graduateFlowClient{
		userPublicKey:     userPublicKey,
		revokedKeyIndices: revokedKeyIndices,
	}
}

func (c *graduateFlowClient) ExecuteScriptAtLatestBlock(ctx context.Context, script []byte, arguments []cadence.Value) (cadence.Value, error) {
	panic("not used")
}

func (c *graduateFlowClient) GetAccount(ctx context.Context, address flow.Address) (*flow.Account, error) {
	revokedSet := make(map[uint32]struct{}, len(c.revokedKeyIndices))
	for _, idx := range c.revokedKeyIndices {
		revokedSet[idx] = struct{}{}
	}

	account := &flow.Account{Address: address}
	for idx := range revokedSet {
		key := flow.NewAccountKey().
			SetPublicKey(c.userPublicKey).
			SetHashAlgo(crypto.SHA3_256).
			SetWeight(flow.AccountKeyWeightThreshold)
		key.Index = idx
		key.Revoked = true
		account.Keys = append(account.Keys, key)
	}

	if c.userPublicKey != nil {
		key := flow.NewAccountKey().
			SetPublicKey(c.userPublicKey).
			SetHashAlgo(crypto.SHA3_256).
			SetWeight(flow.AccountKeyWeightThreshold)
		key.Index = uint32(len(account.Keys))
		account.Keys = append(account.Keys, key)
	}

	return account, nil
}

func (c *graduateFlowClient) GetAccountAtLatestBlock(ctx context.Context, address flow.Address) (*flow.Account, error) {
	panic("not used")
}

func (c *graduateFlowClient) GetTransaction(ctx context.Context, txID flow.Identifier) (*flow.Transaction, error) {
	panic("not used")
}

func (c *graduateFlowClient) GetTransactionResult(ctx context.Context, txID flow.Identifier) (*flow.TransactionResult, error) {
	return &flow.TransactionResult{Status: flow.TransactionStatusSealed}, nil
}

func (c *graduateFlowClient) GetLatestBlockHeader(ctx context.Context, isSealed bool) (*flow.BlockHeader, error) {
	return &flow.BlockHeader{ID: flow.Identifier{}}, nil
}

func (c *graduateFlowClient) GetEventsForHeightRange(ctx context.Context, eventType string, startHeight uint64, endHeight uint64) ([]flow.BlockEvents, error) {
	panic("not used")
}

func (c *graduateFlowClient) SendTransaction(ctx context.Context, tx flow.Transaction) error {
	return nil
}

type testTransientError struct {
	msg string
}

func (e *testTransientError) Error() string   { return e.msg }
func (e *testTransientError) Timeout() bool   { return true }
func (e *testTransientError) Temporary() bool { return true }

var _ net.Error = (*testTransientError)(nil)

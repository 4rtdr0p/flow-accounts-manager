package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/accounts"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/crypto"
)

func TestEmulatorAcceptsSignedTransaction(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	accHandler := handlers.NewAccounts(svcs.GetAccounts())
	txHandler := handlers.NewTransactions(svcs.GetTransactions(), svcs.GetAccounts())

	router := mux.NewRouter()
	router.Handle("/", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/{address}/graduate-to-self-custody", accHandler.GraduateToSelfCustody()).Methods(http.MethodPost)
	router.Handle("/{address}/sign", txHandler.Sign()).Methods(http.MethodPost)

	// Create signing account.
	var account accounts.Account
	res := send(router, http.MethodPost, "/?sync=true", nil)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(t, res, &account)

	// Transaction:
	code := "transaction(greeting: String) { prepare(signer: &Account) {} execute { log(greeting.concat(\", World!\")) }}"
	args := "[{\"type\":\"String\",\"value\":\"Hello\"}]"

	// Sign it.
	body := bytes.NewBufferString(fmt.Sprintf("{\"code\":%q,\"arguments\":%s}", code, args))
	res = send(router, http.MethodPost, fmt.Sprintf("/%s/sign", account.Address), body)
	assertStatusCode(t, res, http.StatusCreated)

	var txResp transactions.SignedTransactionJSONResponse
	fromJsonBody(t, res, &txResp)

	tx := flow.NewTransaction().
		SetScript([]byte(txResp.Code)).
		SetReferenceBlockID(flow.HexToID(txResp.ReferenceBlockID)).
		SetGasLimit(txResp.GasLimit).
		SetProposalKey(flow.HexToAddress(txResp.ProposalKey.Address), txResp.ProposalKey.KeyIndex, txResp.ProposalKey.SequenceNumber).
		SetPayer(flow.HexToAddress(txResp.Payer))

	for _, arg := range txResp.Arguments {
		tx.AddRawArgument(arg) // nolint
	}

	for _, a := range txResp.Authorizers {
		tx.AddAuthorizer(flow.HexToAddress(a))
	}

	for _, s := range txResp.PayloadSignatures {
		bs, err := hex.DecodeString(s.Signature)
		if err != nil {
			t.Fatal(err)
		}
		tx.AddPayloadSignature(flow.HexToAddress(s.Address), s.KeyIndex, bs)
	}

	for _, s := range txResp.EnvelopeSignatures {
		bs, err := hex.DecodeString(s.Signature)
		if err != nil {
			t.Fatal(err)
		}
		tx.AddEnvelopeSignature(flow.HexToAddress(s.Address), s.KeyIndex, bs)
	}

	ctx := context.Background()
	client := test.NewFlowClient(t, cfg)
	_, err := flow_helpers.SendAndWait(ctx, client, *tx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGraduateToSelfCustody(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)
	fc := test.NewFlowClient(t, cfg)

	accHandler := handlers.NewAccounts(svcs.GetAccounts())
	txHandler := handlers.NewTransactions(svcs.GetTransactions(), svcs.GetAccounts())

	router := mux.NewRouter()
	router.Handle("/", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/{address}/graduate-to-self-custody", accHandler.GraduateToSelfCustody()).Methods(http.MethodPost)
	router.Handle("/{address}/sign", txHandler.Sign()).Methods(http.MethodPost)
	router.Handle("/{address}/transactions", txHandler.Create()).Methods(http.MethodPost)

	var account accounts.Account
	res := send(router, http.MethodPost, "/?sync=true", nil)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(t, res, &account)

	beforeGraduate, err := fc.GetAccount(context.Background(), flow.HexToAddress(account.Address))
	if err != nil {
		t.Fatal(err)
	}

	custodialIndices := make([]uint32, 0, len(beforeGraduate.Keys))
	for _, key := range beforeGraduate.Keys {
		if !key.Revoked {
			custodialIndices = append(custodialIndices, key.Index)
		}
	}
	if len(custodialIndices) == 0 {
		t.Fatal("expected custodial account to have active keys before graduation")
	}

	userPublicKey := newTestPublicKeyHex(t)

	graduateBody := bytes.NewBufferString(fmt.Sprintf("{\"userPublicKey\":%q}", userPublicKey))
	res = send(router, http.MethodPost, fmt.Sprintf("/%s/graduate-to-self-custody", account.Address), graduateBody)
	assertStatusCode(t, res, http.StatusOK)

	var graduateResp handlers.GraduateAccountResponse
	fromJsonBody(t, res, &graduateResp)

	if graduateResp.Status != "graduated" {
		t.Fatalf("expected status %q, got %q", "graduated", graduateResp.Status)
	}
	if graduateResp.Type != accounts.AccountTypeNonCustodial {
		t.Fatalf("expected type %q, got %q", accounts.AccountTypeNonCustodial, graduateResp.Type)
	}

	updatedAccount, err := svcs.GetAccounts().Details(account.Address)
	if err != nil {
		t.Fatal(err)
	}
	if updatedAccount.Type != accounts.AccountTypeNonCustodial {
		t.Fatalf("expected stored account type %q, got %q", accounts.AccountTypeNonCustodial, updatedAccount.Type)
	}

	onChainAccount, err := fc.GetAccount(context.Background(), flow.HexToAddress(account.Address))
	if err != nil {
		t.Fatal(err)
	}

	var foundUserKey bool
	for _, key := range onChainAccount.Keys {
		if key.PublicKey.String() == userPublicKey && !key.Revoked {
			foundUserKey = true
		}
	}
	if !foundUserKey {
		t.Fatal("expected graduated account to include active user key")
	}

	onChainByIndex := make(map[uint32]flow.AccountKey, len(onChainAccount.Keys))
	for _, key := range onChainAccount.Keys {
		onChainByIndex[key.Index] = *key
	}
	for _, idx := range custodialIndices {
		key, ok := onChainByIndex[idx]
		if !ok {
			t.Fatalf("expected custodial key index %d on chain", idx)
		}
		if !key.Revoked {
			t.Fatalf("expected custodial key index %d to be revoked", idx)
		}
	}

	rawTxBody := bytes.NewBufferString(`{"code":"transaction() { prepare(signer: &Account) {} execute {} }","arguments":[]}`)
	res = send(router, http.MethodPost, fmt.Sprintf("/%s/sign", account.Address), rawTxBody)
	assertStatusCode(t, res, http.StatusForbidden)

	res = send(router, http.MethodPost, fmt.Sprintf("/%s/transactions", account.Address), bytes.NewBuffer(rawTxBody.Bytes()))
	assertStatusCode(t, res, http.StatusForbidden)
}

func TestGraduateToSelfCustodyAlreadyGraduated(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	accHandler := handlers.NewAccounts(svcs.GetAccounts())

	router := mux.NewRouter()
	router.Handle("/", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/{address}/graduate-to-self-custody", accHandler.GraduateToSelfCustody()).Methods(http.MethodPost)

	var account accounts.Account
	res := send(router, http.MethodPost, "/?sync=true", nil)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(t, res, &account)

	userPublicKey := newTestPublicKeyHex(t)
	graduateBody := bytes.NewBufferString(fmt.Sprintf("{\"userPublicKey\":%q}", userPublicKey))

	res = send(router, http.MethodPost, fmt.Sprintf("/%s/graduate-to-self-custody", account.Address), graduateBody)
	assertStatusCode(t, res, http.StatusOK)

	res = send(router, http.MethodPost, fmt.Sprintf("/%s/graduate-to-self-custody", account.Address), graduateBody)
	assertStatusCode(t, res, http.StatusBadRequest)
}

func TestGraduateToSelfCustodyInvalidPublicKey(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	accHandler := handlers.NewAccounts(svcs.GetAccounts())

	router := mux.NewRouter()
	router.Handle("/", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/{address}/graduate-to-self-custody", accHandler.GraduateToSelfCustody()).Methods(http.MethodPost)

	var account accounts.Account
	res := send(router, http.MethodPost, "/?sync=true", nil)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(t, res, &account)

	body := bytes.NewBufferString(`{"userPublicKey":"not-a-valid-key"}`)
	res = send(router, http.MethodPost, fmt.Sprintf("/%s/graduate-to-self-custody", account.Address), body)
	assertStatusCode(t, res, http.StatusBadRequest)
}

func TestWatchlistAccountManagement(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)
	fc := test.NewFlowClient(t, cfg)
	km := svcs.GetKeyManager()

	accHandler := handlers.NewAccounts(svcs.GetAccounts())

	router := mux.NewRouter()
	router.Handle("/", accHandler.AddNonCustodialAccount()).Methods(http.MethodPost)
	router.Handle("/{address}", accHandler.Details()).Methods(http.MethodGet)
	router.Handle("/{address}", accHandler.DeleteNonCustodialAccount()).Methods(http.MethodDelete)

	// Create a non-custodial account.
	adminAuthorizer, err := km.AdminAuthorizer(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	nonCustodialAccount := test.NewFlowAccount(t, fc, adminAuthorizer.Address, adminAuthorizer.Key, adminAuthorizer.Signer)

	// Add created non-custodial account to watchlist.
	account := accounts.Account{Address: nonCustodialAccount.Address.Hex()}
	buf := bytes.NewBuffer(asJson(&account))
	res := send(router, http.MethodPost, "/", buf)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(t, res, &account)

	// Ensure that account can be found.
	res = send(router, http.MethodGet, fmt.Sprintf("/%s", account.Address), nil)
	assertStatusCode(t, res, http.StatusOK)
	fromJsonBody(t, res, &account)

	if account.Address != flow_helpers.FormatAddress(nonCustodialAccount.Address) {
		t.Fatalf("read account address doesn't match - expected %q, got %q", flow_helpers.FormatAddress(nonCustodialAccount.Address), account.Address)
	}

	if account.Type != accounts.AccountTypeNonCustodial {
		t.Fatalf("read account type doesn't match - expected %q, got %q", accounts.AccountTypeNonCustodial, account.Type)
	}

	// Remove the non-custodial account from watchlist.
	res = send(router, http.MethodDelete, fmt.Sprintf("/%s", account.Address), nil)
	assertStatusCode(t, res, http.StatusOK)

	// Ensure that it's not found anymore.
	res = send(router, http.MethodGet, fmt.Sprintf("/%s", account.Address), nil)
	assertStatusCode(t, res, http.StatusNotFound)
}

func assertStatusCode(t *testing.T, res *http.Response, expected int) {
	t.Helper()
	if res.StatusCode != expected {
		bs, err := io.ReadAll(res.Body)
		if err != nil {
			panic(err)
		}
		t.Fatalf("expected HTTP response status code %d, got %d: %s", expected, res.StatusCode, string(bs))
	}
}

func asJson(v interface{}) []byte {
	if v == nil {
		return nil
	}
	bs, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bs
}

func fromJsonBody(t *testing.T, res *http.Response, v interface{}) {
	t.Helper()

	bs, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(bs, v)
	if err != nil {
		t.Fatal(err)
	}
}

func send(router *mux.Router, method, path string, body io.Reader) *http.Response {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Result()
}

func newTestPublicKeyHex(t *testing.T) string {
	t.Helper()

	seed := make([]byte, 64)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}

	privateKey, err := crypto.GeneratePrivateKey(crypto.ECDSA_P256, seed)
	if err != nil {
		t.Fatal(err)
	}

	return privateKey.PublicKey().String()
}

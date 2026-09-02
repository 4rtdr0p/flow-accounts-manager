package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"

	"github.com/flow-hydraulics/flow-wallet-api/example"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/templates"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/flow-hydraulics/flow-wallet-api/tokens"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
)

// TestE2EChainedHappyPath (T1) is the single end-to-end happy path a reviewer
// runs to see the custodial lifecycle work against a real emulator: create a
// custodial account over HTTP and poll its job to COMPLETE, set it up
// (FlowToken/FUSD/ExampleNFT vaults), mint it an NFT, move FLOW to a second
// (watchlisted) account and see the chain-event listener record the deposit,
// then rotate the account's key and confirm the old key is revoked on-chain.
// One account, one continuous chain of real transactions.
func TestE2EChainedHappyPath(t *testing.T) {
	cfg := test.LoadConfig(t)
	app := test.GetServices(t, cfg)

	accountSvc := app.GetAccounts()
	tokenSvc := app.GetTokens()
	templateSvc := app.GetTemplates()
	txSvc := app.GetTransactions()
	jobSvc := app.GetJobs()
	fc := app.GetFlowClient()
	km := app.GetKeyManager()

	ctx := context.Background()

	// --- Fixtures: FUSD + ExampleNFT deployed & enabled on the admin account,
	// exactly as the token/setup handler tests do. ---
	fusd, err := templateSvc.GetTokenByName("FUSD")
	fatal(t, err)
	fatal(t, tokenSvc.DeployTokenContractForAccount(ctx, true, fusd.Name, fusd.Address))

	setupBytes, err := os.ReadFile(filepath.Join(testCadenceTxBasePath, "setup_exampleNFT.cdc"))
	fatal(t, err)
	transferBytes, err := os.ReadFile(filepath.Join(testCadenceTxBasePath, "transfer_exampleNFT.cdc"))
	fatal(t, err)
	balanceBytes, err := os.ReadFile(filepath.Join(testCadenceTxBasePath, "balance_exampleNFT.cdc"))
	fatal(t, err)
	mintBytes, err := os.ReadFile(filepath.Join(testCadenceTxBasePath, "mint_exampleNFT.cdc"))
	fatal(t, err)

	exampleNft := templates.Token{
		Name:     "ExampleNFT",
		Address:  cfg.AdminAddress,
		Type:     templates.NFT,
		Setup:    string(setupBytes),
		Transfer: string(transferBytes),
		Balance:  string(balanceBytes),
	}
	fatal(t, templateSvc.AddToken(&exampleNft))
	fatal(t, tokenSvc.DeployTokenContractForAccount(ctx, true, exampleNft.Name, exampleNft.Address))

	pluginDeps := plugins.PluginDeps{
		Accounts:     accountSvc,
		Tokens:       tokenSvc,
		Transactions: txSvc,
		Config:       cfg,
	}

	accHandler := handlers.NewAccounts(accountSvc)
	jobHandler := handlers.NewJobs(jobSvc)
	router := mux.NewRouter()
	router.Handle("/accounts", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/jobs/{jobId}", jobHandler.Details()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/setup", example.NewSetupHandler(pluginDeps)).Methods(http.MethodPost)

	// --- Step 1: create a custodial account async, poll its job to COMPLETE. ---
	createRes := handleStepRequest(httpTestStep{
		name:     "create account async",
		method:   http.MethodPost,
		url:      "/accounts",
		expected: `(?m)^{"jobId":".+"}$`,
		status:   http.StatusCreated,
	}, router, t)

	var rJob jobs.JSONResponse
	fatal(t, json.Unmarshal(createRes.Body.Bytes(), &rJob))
	job, err := test.WaitForJob(jobSvc, rJob.ID.String())
	fatal(t, err)

	handleStepRequest(httpTestStep{
		name:     "poll job COMPLETE",
		method:   http.MethodGet,
		url:      fmt.Sprintf("/jobs/%s", rJob.ID.String()),
		expected: `"state":"COMPLETE"`,
		status:   http.StatusOK,
	}, router, t)

	accountA := job.Result
	if _, err := flow_helpers.ValidateAddress(accountA, flow.Emulator); err != nil {
		t.Fatalf("job produced an invalid account address %q: %v", accountA, err)
	}
	t.Logf("account A = %s", accountA)

	// --- Step 2: set the account up (FlowToken/FUSD/ExampleNFT vaults). ---
	handleStepRequest(httpTestStep{
		name:     "setup account (sync)",
		method:   http.MethodPost,
		url:      fmt.Sprintf("/accounts/%s/setup", accountA),
		expected: `(?m)^{"transactionId":".+"}$`,
		status:   http.StatusCreated,
		sync:     true,
	}, router, t)

	// --- Step 3: mint an ExampleNFT to the account, assert it now holds one. ---
	mintCode, err := templates.TokenCode(cfg.ChainID, &exampleNft, string(mintBytes))
	fatal(t, err)
	_, _, err = txSvc.Create(ctx, true, cfg.AdminAddress, mintCode,
		[]transactions.Argument{cadence.NewAddress(flow.HexToAddress(accountA))},
		transactions.General)
	fatal(t, err)

	nftDetails, err := tokenSvc.Details(ctx, exampleNft.Name, accountA)
	fatal(t, err)
	nftIDs := nftDetails.Balance.CadenceValue.(cadence.Array).Values
	if len(nftIDs) == 0 {
		t.Fatal("expected account A to hold at least one ExampleNFT after mint")
	}
	t.Logf("account A holds %d ExampleNFT(s)", len(nftIDs))

	// --- Step 4: fund A, then move FLOW A -> B and see the chain-event
	// listener record the deposit. B is a watchlisted (non-custodial) account
	// so the deposit-tracking listener observes and stores the incoming FLOW. ---
	_, _, err = tokenSvc.CreateWithdrawal(ctx, true, cfg.AdminAddress, tokens.WithdrawalRequest{
		TokenName: "FlowToken",
		Recipient: accountA,
		FtAmount:  "10.0",
	})
	fatal(t, err)

	adminAuthorizer, err := km.AdminAuthorizer(ctx)
	fatal(t, err)
	accountB := test.NewFlowAccount(t, fc, adminAuthorizer.Address, adminAuthorizer.Key, adminAuthorizer.Signer)
	_, err = accountSvc.AddNonCustodialAccount(accountB.Address.Hex())
	fatal(t, err)
	t.Logf("account B (watchlist) = %s", accountB.Address.Hex())

	_, transferTx, err := tokenSvc.CreateWithdrawal(ctx, true, accountA, tokens.WithdrawalRequest{
		TokenName: "FlowToken",
		Recipient: accountB.Address.Hex(),
		FtAmount:  "1.0",
	})
	fatal(t, err)
	if flow.HexToID(transferTx.TransactionId) == flow.EmptyID {
		t.Fatal("expected a non-empty transfer transaction id")
	}

	// The listener runs on a ~1s poll; wait for it to see the deposit.
	var sawDeposit bool
	for i := 0; i < 50; i++ {
		deposits, err := tokenSvc.ListDeposits("0x"+accountB.Address.Hex(), "FlowToken")
		fatal(t, err)
		if len(deposits) > 0 {
			sawDeposit = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !sawDeposit {
		t.Fatal("chain-event listener never recorded the FLOW deposit to account B")
	}

	// --- Step 5: rotate account A's key; the old key must be revoked on-chain. ---
	beforeRotate, err := accountSvc.Details(accountA)
	fatal(t, err)
	if len(beforeRotate.Keys) == 0 {
		t.Fatal("expected account A to have a managed key before rotation")
	}
	oldKeyIndex := beforeRotate.Keys[0].Index

	_, rotateResult, err := accountSvc.RotateKey(ctx, true, accountA)
	fatal(t, err)
	if !rotateResult.OldKeyRevoked {
		t.Fatal("expected rotate result OldKeyRevoked=true")
	}
	if rotateResult.NewKeyIndex <= rotateResult.OldKeyIndex {
		t.Fatalf("expected new key index > old, got new=%d old=%d", rotateResult.NewKeyIndex, rotateResult.OldKeyIndex)
	}

	onChain, err := fc.GetAccount(ctx, flow.HexToAddress(accountA))
	fatal(t, err)
	if int(oldKeyIndex) >= len(onChain.Keys) || !onChain.Keys[oldKeyIndex].Revoked {
		t.Fatalf("expected old key index %d to be revoked on-chain", oldKeyIndex)
	}
	if int(rotateResult.NewKeyIndex) >= len(onChain.Keys) || onChain.Keys[rotateResult.NewKeyIndex].Revoked {
		t.Fatalf("expected new key index %d to be active on-chain", rotateResult.NewKeyIndex)
	}
	t.Logf("rotated: old key %d revoked, new key %d active", oldKeyIndex, rotateResult.NewKeyIndex)
}

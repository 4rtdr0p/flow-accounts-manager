package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
)

func TestW03ArtistActivationAndCommunityPoolFlow(t *testing.T) {
	cfg := test.LoadConfig(t)
	app := test.GetServices(t, cfg)
	svc := app.GetAccounts()

	_, account, err := svc.Create(context.Background(), true)
	fatal(t, err)

	activated, err := svc.ActivateArtist(account.Address)
	fatal(t, err)

	if !activated.IsArtist {
		t.Fatal("expected artist account to be activated")
	}

	if activated.CommunityPoolAddress != "" {
		t.Fatal("artist activation should not create a community pool address")
	}

	pooled, err := svc.EnableCommunityPool(context.Background(), account.Address)
	fatal(t, err)

	if !pooled.IsArtist {
		t.Fatal("expected account to remain marked as artist")
	}

	if pooled.CommunityPoolAddress == "" {
		t.Fatal("expected community pool address to be created")
	}

	if pooled.CommunityPoolAddress == pooled.Address {
		t.Fatal("expected community pool address to differ from artist address")
	}

	poolAccount, err := svc.Details(pooled.CommunityPoolAddress)
	fatal(t, err)

	if poolAccount.Address != pooled.CommunityPoolAddress {
		t.Fatalf("expected community pool account %s, got %s", pooled.CommunityPoolAddress, poolAccount.Address)
	}

	if poolAccount.IsArtist {
		t.Fatal("expected community pool account to remain a regular custodial account")
	}

	pooledAgain, err := svc.EnableCommunityPool(context.Background(), account.Address)
	fatal(t, err)

	if pooledAgain.CommunityPoolAddress != pooled.CommunityPoolAddress {
		t.Fatal("expected community pool enable to be idempotent once address exists")
	}
}

func TestW03CommunityPoolRequiresArtistActivation(t *testing.T) {
	cfg := test.LoadConfig(t)
	app := test.GetServices(t, cfg)
	svc := app.GetAccounts()

	_, account, err := svc.Create(context.Background(), true)
	fatal(t, err)

	_, err = svc.EnableCommunityPool(context.Background(), account.Address)
	if err == nil {
		t.Fatal("expected enabling community pool without artist activation to fail")
	}

	if !strings.Contains(err.Error(), "artist account must be activated") {
		t.Fatalf("expected artist activation error, got %v", err)
	}
}

func TestW03ArtistActivationHandler(t *testing.T) {
	cfg := test.LoadConfig(t)
	app := test.GetServices(t, cfg)
	svc := app.GetAccounts()
	handler := handlers.NewAccounts(svc)

	_, account, err := svc.Create(context.Background(), true)
	fatal(t, err)

	router := buildRouter(routeOptions{}, routeHandlers{
		System:           handlers.NewSystem(app.GetSystem()),
		Templates:        handlers.NewTemplates(app.GetTemplates()),
		Jobs:             handlers.NewJobs(app.GetJobs()),
		Accounts:         handler,
		Transactions:     handlers.NewTransactions(app.GetTransactions()),
		Tokens:           handlers.NewTokens(app.GetTokens()),
		Ops:              handlers.NewOps(app.GetOps()),
		DebugURL:         "debug-url",
		DebugSHA:         "debug-sha",
		DebugBuildTime:   "debug-build-time",
		WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
	}, nil, plugins.PluginDeps{})

	req := httptest.NewRequest(http.MethodPost, "/v1/accounts/"+account.Address+"/artist-activate", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	refreshed, err := svc.Details(account.Address)
	fatal(t, err)

	if !refreshed.IsArtist {
		t.Fatal("expected artist flag to be persisted after handler call")
	}

	if refreshed.CommunityPoolAddress != "" {
		t.Fatal("artist activation handler should not create community pool address")
	}
}

func TestW03CommunityPoolEnableHandler(t *testing.T) {
	cfg := test.LoadConfig(t)
	app := test.GetServices(t, cfg)
	svc := app.GetAccounts()
	handler := handlers.NewAccounts(svc)

	_, account, err := svc.Create(context.Background(), true)
	fatal(t, err)

	_, err = svc.ActivateArtist(account.Address)
	fatal(t, err)

	router := buildRouter(routeOptions{}, routeHandlers{
		System:           handlers.NewSystem(app.GetSystem()),
		Templates:        handlers.NewTemplates(app.GetTemplates()),
		Jobs:             handlers.NewJobs(app.GetJobs()),
		Accounts:         handler,
		Transactions:     handlers.NewTransactions(app.GetTransactions()),
		Tokens:           handlers.NewTokens(app.GetTokens()),
		Ops:              handlers.NewOps(app.GetOps()),
		DebugURL:         "debug-url",
		DebugSHA:         "debug-sha",
		DebugBuildTime:   "debug-build-time",
		WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
	}, nil, plugins.PluginDeps{})

	req := httptest.NewRequest(http.MethodPost, "/v1/accounts/"+account.Address+"/community-pool-enable", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	refreshed, err := svc.Details(account.Address)
	fatal(t, err)

	if refreshed.CommunityPoolAddress == "" {
		t.Fatal("expected community pool address to be persisted after handler call")
	}
}

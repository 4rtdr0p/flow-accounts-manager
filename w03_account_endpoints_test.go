package main

import (
	"context"
	"strings"
	"testing"

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

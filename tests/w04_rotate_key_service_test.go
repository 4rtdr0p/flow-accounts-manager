package tests

import (
	"context"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/onflow/flow-go-sdk"
)

func TestW04RotateKeySyncRevokesOldKeyAndSoftDeletesDBRow(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	_, account, err := svcs.GetAccounts().Create(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}

	if len(account.Keys) == 0 {
		t.Fatal("expected managed key on created account")
	}
	oldKeyID := account.Keys[0].ID
	oldKeyIndex := account.Keys[0].Index

	_, result, err := svcs.GetAccounts().RotateKey(context.Background(), true, account.Address)
	if err != nil {
		t.Fatal(err)
	}

	if !result.OldKeyRevoked {
		t.Fatal("expected oldKeyRevoked=true")
	}
	if result.OldKeyIndex != oldKeyIndex {
		t.Fatalf("expected old key index %d, got %d", oldKeyIndex, result.OldKeyIndex)
	}
	if result.NewKeyIndex <= result.OldKeyIndex {
		t.Fatalf("expected new key index > old key index, got new=%d old=%d", result.NewKeyIndex, result.OldKeyIndex)
	}

	flowAccount, err := svcs.GetFlowClient().GetAccount(context.Background(), flow.HexToAddress(account.Address))
	if err != nil {
		t.Fatal(err)
	}
	if oldKeyIndex >= len(flowAccount.Keys) || !flowAccount.Keys[oldKeyIndex].Revoked {
		t.Fatalf("expected old key index %d to be revoked on-chain", oldKeyIndex)
	}
	if result.NewKeyIndex >= len(flowAccount.Keys) || flowAccount.Keys[result.NewKeyIndex].Revoked {
		t.Fatalf("expected new key index %d to be active on-chain", result.NewKeyIndex)
	}

	var oldKeyRow keys.Storable
	if err := svcs.GetDB().Unscoped().First(&oldKeyRow, oldKeyID).Error; err != nil {
		t.Fatal(err)
	}
	if !oldKeyRow.DeletedAt.Valid {
		t.Fatal("expected old key row DeletedAt to be set")
	}

	rotatedAccount, err := svcs.GetAccounts().Details(account.Address)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotatedAccount.Keys) != 1 {
		t.Fatalf("expected 1 active managed key after rotation, got %d", len(rotatedAccount.Keys))
	}
	if rotatedAccount.Keys[0].Index != result.NewKeyIndex {
		t.Fatalf("expected active managed key index %d, got %d", result.NewKeyIndex, rotatedAccount.Keys[0].Index)
	}
}

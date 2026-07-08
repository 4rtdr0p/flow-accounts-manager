package accounts

import (
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGormStoreRotateKeyStateRollsBackOnInsertFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Account{}, &keys.Storable{}); err != nil {
		t.Fatal(err)
	}

	store := NewGormStore(db)
	oldKey := keys.Storable{
		AccountAddress: "0x01",
		Index:          0,
		Type:           keys.AccountKeyTypeLocal,
		PublicKey:      "0xabc",
		SignAlgo:       "ECDSA_P256",
		HashAlgo:       "SHA3_256",
	}
	if err := store.InsertKey(&oldKey); err != nil {
		t.Fatal(err)
	}

	conflictingNewKey := keys.Storable{
		ID:             oldKey.ID,
		AccountAddress: "0x01",
		Index:          1,
		Type:           keys.AccountKeyTypeLocal,
		PublicKey:      "0xdef",
		SignAlgo:       "ECDSA_P256",
		HashAlgo:       "SHA3_256",
	}

	if err := store.RotateKeyState(oldKey.ID, &conflictingNewKey); err == nil {
		t.Fatal("expected insert conflict, got nil")
	}

	var persisted keys.Storable
	if err := db.First(&persisted, oldKey.ID).Error; err != nil {
		t.Fatalf("expected old key to remain visible after rollback: %v", err)
	}
	if persisted.DeletedAt.Valid {
		t.Fatal("expected old key rollback to keep DeletedAt unset")
	}
}

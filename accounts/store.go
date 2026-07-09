package accounts

import (
	"github.com/flow-hydraulics/flow-wallet-api/datastore"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
)

// Store manages data regarding accounts.
type Store interface {
	// List all accounts.
	Accounts(datastore.ListOptions) ([]Account, error)

	// Get account details.
	Account(address string) (Account, error)

	// Insert a new account.
	InsertAccount(a *Account) error

	// Update an existing account.
	SaveAccount(a *Account) error

	// Insert a new account key.
	InsertKey(k *keys.Storable) error

	// ArchiveKey soft-deletes a key row for audit retention.
	ArchiveKey(id int) error

	// RotateKeyState atomically archives the old key row and inserts the new key row.
	RotateKeyState(oldKeyID int, newKey *keys.Storable) error

	// Permanently delete an account, despite of `DeletedAt` field.
	HardDeleteAccount(a *Account) error
}

package accounts

import (
	"github.com/flow-hydraulics/flow-wallet-api/datastore"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"gorm.io/gorm"
)

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) Store {
	return &GormStore{db}
}

func (s *GormStore) Accounts(o datastore.ListOptions) (aa []Account, err error) {
	err = s.db.
		Order("created_at desc").
		Limit(o.Limit).
		Offset(o.Offset).
		Find(&aa).Error
	return
}

func (s *GormStore) Account(address string) (a Account, err error) {
	err = s.db.Preload("Keys").First(&a, "address = ?", address).Error
	return
}

func (s *GormStore) InsertAccount(a *Account) error {
	return s.db.Create(a).Error
}

func (s *GormStore) SaveAccount(a *Account) error {
	return s.db.Save(&a).Error
}

func (s *GormStore) InsertKey(k *keys.Storable) error {
	return s.db.Create(k).Error
}

func (s *GormStore) ArchiveKey(id int) error {
	return s.db.Delete(&keys.Storable{}, id).Error
}

func (s *GormStore) RotateKeyState(oldKeyID int, newKey *keys.Storable) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&keys.Storable{}, oldKeyID).Error; err != nil {
			return err
		}
		if err := tx.Create(newKey).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *GormStore) HardDeleteAccount(a *Account) error {
	return s.db.Unscoped().Delete(a).Error
}

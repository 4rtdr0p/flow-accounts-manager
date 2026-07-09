package m20260619

import "gorm.io/gorm"

const ID = "20260619"

type Account struct {
	Address              string `gorm:"primaryKey"`
	IsArtist             bool `gorm:"default:false"`
	CommunityPoolAddress string
}

func Migrate(tx *gorm.DB) error {
	if err := tx.Migrator().AddColumn(&Account{}, "is_artist"); err != nil {
		return err
	}

	if err := tx.Migrator().AddColumn(&Account{}, "community_pool_address"); err != nil {
		return err
	}

	return nil
}

func Rollback(tx *gorm.DB) error {
	if err := tx.Migrator().DropColumn(&Account{}, "community_pool_address"); err != nil {
		return err
	}

	if err := tx.Migrator().DropColumn(&Account{}, "is_artist"); err != nil {
		return err
	}

	return nil
}

package gorm

import (
	"fmt"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/migrations"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	dbTypePostgresql = "psql"
	dbTypeMysql      = "mysql"
	dbTypeSqlite     = "sqlite"
)

// New opens the database configured by cfg and migrates it to the latest
// schema version. pluginMigrations are appended to the core migration list
// (migrations.List) so they run in the same gormigrate table as the core
// migrations, keeping rollback and versioning coherent across the whole
// schema. Each plugin that owns tables exposes its migrations as a
// package-level func (e.g. artdrop.Migrations) that the caller collects and
// passes in here, since migrations are static and can be built before the
// plugin instances that need this DB handle exist. Execution order is
// positional — core migrations first, then pluginMigrations in the order
// given — not sorted by ID, so callers should keep passing plugins in a
// stable order and keep each plugin's own migration IDs timestamp-prefixed.
func New(cfg *configs.Config, pluginMigrations ...*gormigrate.Migration) (*gorm.DB, error) {
	// TODO(latenssi): safeguard against nil config?

	var dialector gorm.Dialector
	switch cfg.DatabaseType {
	default:
		panic(fmt.Sprintf("database type '%s' not supported", cfg.DatabaseType))
	case dbTypePostgresql:
		dialector = postgres.Open(cfg.DatabaseDSN)
	case dbTypeMysql:
		dialector = mysql.Open(cfg.DatabaseDSN)
	case dbTypeSqlite:
		dialector = sqlite.Open(cfg.DatabaseDSN)
	}

	options := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	db, err := gorm.Open(dialector, options)
	if err != nil {
		return nil, err
	}

	if cfg.DatabaseType == dbTypeSqlite {
		// SQLite doesn't handle multiple pooled connections well; pin to one.
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(1)
	}

	allMigrations := append(migrations.List(), pluginMigrations...)
	m := gormigrate.New(db, gormigrate.DefaultOptions, allMigrations)
	if cfg.DatabaseVersion == "" {
		err = m.Migrate()
	} else {
		err = m.MigrateTo(cfg.DatabaseVersion)
		if err != nil {
			return nil, err
		}

		err = m.RollbackTo(cfg.DatabaseVersion)
	}
	if err != nil {
		return nil, err
	}

	return db, nil
}

func Close(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		panic("unable to close database")
	}

	if err := sqlDB.Close(); err != nil {
		panic("unable to close database")
	}
}

package database

import (
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func RunMigrations(connStr, migrationsPath string) error {
	// Use a separate migrations table so rides and pathfinder don't conflict
	// when sharing the same PostgreSQL database.
	sep := "&"
	if !strings.Contains(connStr, "?") {
		sep = "?"
	}
	migrateConn := connStr + sep + "x-migrations-table=rides_schema_migrations"

	m, err := migrate.New("file://"+migrationsPath, migrateConn)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

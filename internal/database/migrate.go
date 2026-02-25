package database

import (
	"errors"
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

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			return nil
		}
		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			// A previous migration attempt failed mid-way. Reset to the last
			// clean version and retry so the migration runs from scratch.
			if forceErr := m.Force(dirtyErr.Version - 1); forceErr != nil {
				return fmt.Errorf("forcing version after dirty state: %w", forceErr)
			}
			if retryErr := m.Up(); retryErr != nil && retryErr != migrate.ErrNoChange {
				return fmt.Errorf("running migrations after dirty reset: %w", retryErr)
			}
			return nil
		}
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

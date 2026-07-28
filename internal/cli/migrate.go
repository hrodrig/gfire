package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// MigrationsPath is the default path to the postgres migration files.
const MigrationsPath = "internal/storage/postgres/migrations"

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run PostgreSQL schema migrations",
		Long: `Runs golang-migrate up migrations against the PostgreSQL DSN from config.

Requires the 'migrate' CLI (github.com/golang-migrate/migrate) installed.
Migration files are read from ` + MigrationsPath + `.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := loadConfig()
			if err != nil {
				return err
			}
			if c.Storage.Backend != "postgres" {
				return fmt.Errorf("migrations require storage.backend = postgres (current: %s)", c.Storage.Backend)
			}
			dsn := c.Storage.Postgres.DSN
			if dsn == "" {
				return fmt.Errorf("storage.postgres.dsn is required")
			}

			// Check migrate CLI is available.
			if _, err := exec.LookPath("migrate"); err != nil {
				return fmt.Errorf("migrate CLI not found in PATH. Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest")
			}

			cmd := exec.Command("migrate",
				"-path", MigrationsPath,
				"-database", dsn,
				"up",
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	}
	cmd.Flags().StringVar(&cfgFile, "config", "", "path to gfire.yaml")
	return cmd
}

// Package cli implements the gfire command-line interface (Band 6).
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrodrig/gfire/internal/app"
	"github.com/hrodrig/gfire/internal/config"
)

var cfgFile string
var cfg *config.Config

// Execute runs the root command.
func Execute(version string) {
	root := NewRoot(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// NewRoot builds the cobra command tree.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "gfire",
		Short: "GFire background job service",
	}
	root.AddCommand(newVersionCmd(version))
	root.AddCommand(newServerCmd())
	root.AddCommand(newJobCmd())
	root.AddCommand(newQueueCmd())
	root.AddCommand(newMigrateCmd())
	root.AddCommand(newServerStatusCmd())
	return root
}

func loadConfig() (*config.Config, error) {
	if cfg != nil {
		return cfg, nil
	}
	c, err := config.Load(cfgFile)
	if err != nil {
		return nil, err
	}
	cfg = c
	return cfg, nil
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("gfire %s\n", version)
		},
	}
}

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run GFire daemon (engine + REST API)",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := loadConfig()
			if err != nil {
				return err
			}
			return app.RunServer(context.Background(), c)
		},
	}
	cmd.Flags().StringVar(&cfgFile, "config", "", "path to gfire.yaml")
	return cmd
}

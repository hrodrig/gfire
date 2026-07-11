package cli

import (
	"context"

	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/storage"
)

func openCLIStore(ctx context.Context, c *config.Config) (storage.Storage, error) {
	return config.OpenStorage(ctx, c.Storage, c.Server.ServerID)
}

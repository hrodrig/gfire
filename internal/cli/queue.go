package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "queue", Short: "Inspect queues"}
	cmd.AddCommand(newQueueListCmd())
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to gfire.yaml")
	return cmd
}

func newQueueListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List queues and their depth",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := loadConfig()
			if err != nil {
				return err
			}
			ctx := context.Background()
			store, err := openCLIStore(ctx, c)
			if err != nil {
				return err
			}
			defer store.Close()

			queues, err := store.GetQueues(ctx)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "QUEUE\tDEPTH")
			for _, q := range queues {
				n, _ := store.GetQueueLength(ctx, q)
				fmt.Fprintf(w, "%s\t%d\n", q, n)
			}
			return w.Flush()
		},
	}
}

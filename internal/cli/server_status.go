package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newServerStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show server cluster status",
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

			servers, err := store.GetServers(ctx)
			if err != nil {
				return err
			}

			if len(servers) == 0 {
				fmt.Println("No servers registered.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVER ID\tSTATUS\tWORKERS\tQUEUES\tLAST HEARTBEAT\tUPTIME")
			now := time.Now()
			for _, sv := range servers {
				queues := sv.Queues
				if len(queues) == 0 {
					queues = []string{"default"}
				}
				uptime := now.Sub(sv.StartedAt).Round(time.Second)
				ago := now.Sub(sv.LastHeartbeat).Round(time.Second)
				fmt.Fprintf(w, "%s\t%s\t%d\t%v\t%s ago\t%s\n",
					sv.ID, sv.Status, sv.WorkerCount, queues, ago, uptime)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&cfgFile, "config", "", "path to gfire.yaml")
	return cmd
}

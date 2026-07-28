package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newJobCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "job", Short: "Inspect and manage jobs"}
	cmd.AddCommand(newJobListCmd())
	cmd.AddCommand(newJobGetCmd())
	cmd.AddCommand(newJobRequeueCmd())
	cmd.AddCommand(newJobCancelCmd())
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to gfire.yaml")
	return cmd
}

func newJobListCmd() *cobra.Command {
	var state string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List jobs",
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
			jobs, err := store.GetJobsByState(ctx, state, 0, limit)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tQUEUE\tSTATE")
			for _, jw := range jobs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", jw.Job.ID, jw.Job.Name, jw.Job.Queue, jw.CurrentState())
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by state")
	cmd.Flags().IntVar(&limit, "limit", 20, "max rows")
	return cmd
}

func newJobGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [job-id]",
		Short: "Get job detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			jw, err := store.GetJob(ctx, args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(jw)
		},
	}
}

func newJobRequeueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "requeue [job-id]",
		Short: "Requeue a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			return store.Requeue(ctx, args[0], "cli requeue")
		},
	}
}

func newJobCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [job-id]",
		Short: "Cancel an in-flight job (calls REST API)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := loadConfig()
			if err != nil {
				return err
			}
			url := fmt.Sprintf("http://%s/v1/jobs/%s/cancel", c.ListenAddr(), args[0])
			req, err := http.NewRequest(http.MethodPost, url, nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("cancel request failed: %w (is the server running?)", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("cancel failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			fmt.Printf("Job %s: cancel request sent.\n", args[0])
			return nil
		},
	}
}

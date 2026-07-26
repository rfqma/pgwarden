package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/maruarchive/pgwarden/internal/api"
	"github.com/maruarchive/pgwarden/internal/backup"
	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/restore"
	"github.com/maruarchive/pgwarden/internal/storage"
)

var version = "dev"

func main() {
	godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	root := &cobra.Command{
		Use:     "pgwarden",
		Short:   "PostgreSQL backup guardian",
		Version: version,
	}

	root.AddCommand(
		backupCmd(ctx),
		restoreCmd(ctx),
		listCmd(ctx),
		deleteCmd(ctx),
		serveCmd(ctx),
		healthCmd(ctx),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, error) {
	return config.Load()
}

func createStore(cfg *config.Config) (*storage.S3Storage, error) {
	return storage.New(&cfg.S3)
}

func backupCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Create a backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			store, err := createStore(cfg)
			if err != nil {
				return err
			}

			result, err := backup.Run(ctx, cfg, store)
			if err != nil {
				return err
			}

			fmt.Printf("Backup created: %s\n", result.ID)
			fmt.Printf("  Size: %d bytes\n", result.Size)
			fmt.Printf("  Duration: %s\n", result.Duration)
			fmt.Printf("  Checksum: %s\n", result.Checksum)

			return nil
		},
	}
}

func restoreCmd(ctx context.Context) *cobra.Command {
	var backupID string

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore from a backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupID == "" {
				return fmt.Errorf("--id is required")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			store, err := createStore(cfg)
			if err != nil {
				return err
			}

			return restore.Run(ctx, cfg, store, backupID)
		},
	}

	cmd.Flags().StringVar(&backupID, "id", "", "Backup ID to restore")
	return cmd
}

func listCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			store, err := createStore(cfg)
			if err != nil {
				return err
			}

			objects, err := store.List(ctx)
			if err != nil {
				return err
			}

			if len(objects) == 0 {
				fmt.Println("No backups found.")
				return nil
			}

			for _, obj := range objects {
				fmt.Printf("%s  %d bytes  %s\n",
					*obj.Key, *obj.Size, obj.LastModified.Format("2006-01-02 15:04:05"))
			}

			return nil
		},
	}
}

func deleteCmd(ctx context.Context) *cobra.Command {
	var backupID string

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupID == "" {
				return fmt.Errorf("--id is required")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			store, err := createStore(cfg)
			if err != nil {
				return err
			}

			key := fmt.Sprintf("backups/%s/%s.dump.gpg",
				backupID[:4]+"/"+backupID[4:6], backupID)

			if err := store.Delete(ctx, key); err != nil {
				return err
			}

			fmt.Printf("Backup deleted: %s\n", backupID)
			return nil
		},
	}

	cmd.Flags().StringVar(&backupID, "id", "", "Backup ID to delete")
	return cmd
}

func serveCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			store, err := createStore(cfg)
			if err != nil {
				return err
			}

			server := api.NewServer(cfg, store)
			return server.Start(ctx)
		},
	}
}

func healthCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Health check",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("ok")
			return nil
		},
	}
}

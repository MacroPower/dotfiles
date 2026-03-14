// Sandbox is the CLI entrypoint for sandbox container operations,
// including firewall config generation, user setup, and privilege-dropping
// init.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"go.jacobcolvin.com/niceyaml/fangs"
	"go.jacobcolvin.com/x/log"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func main() {
	logCfg := log.NewConfig()

	rootCmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Sandbox container operations",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			handler, err := logCfg.NewHandler(os.Stderr)
			if err != nil {
				return err
			}

			slog.SetDefault(slog.New(handler))

			return nil
		},
	}

	logCfg.RegisterFlags(rootCmd.PersistentFlags())

	err := logCfg.RegisterCompletions(rootCmd)
	if err != nil {
		slog.Error("registering completions",
			slog.Any("err", err),
		)
	}

	rootCmd.AddCommand(
		&cobra.Command{
			Use:   "generate",
			Short: "Generate iptables/envoy configs from YAML",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return sandbox.Generate(cmd.Context(), sandbox.ConfigPath)
			},
		},
		&cobra.Command{
			Use:   "setup-dev",
			Short: "Fish history symlinks, claude.json persistence, atuin config",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return sandbox.SetupDev()
			},
		},
		&cobra.Command{
			Use:   "setup-user",
			Short: "Create non-root user in /etc/passwd and /etc/group",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return sandbox.SetupUser()
			},
		},
		&cobra.Command{
			Use:   "init [-- cmd...]",
			Short: "Load firewall, start services, drop privileges, exec cmd",
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return sandbox.Init(cmd.Context(), args)
			},
		},
	)

	err = fang.Execute(context.Background(), rootCmd,
		fang.WithErrorHandler(fangs.ErrorHandler),
		fang.WithoutCompletions(),
		fang.WithoutManpage(),
		fang.WithoutVersion(),
	)
	if err != nil {
		var exitErr *sandbox.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}

		os.Exit(1)
	}
}

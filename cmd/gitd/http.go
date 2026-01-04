package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/go-git/go-billy/v6/osfs"
	githttp "github.com/go-git/go-git/v6/backend/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/spf13/cobra"

	serverhttp "github.com/wzshiming/gitd/server/http"
)

var (
	httpPort   int
	httpPrefix string
)

func init() {
	httpCmd.Flags().IntVarP(&httpPort, "port", "p", 8080, "Port to run the HTTP server on")
	httpCmd.Flags().StringVarP(&httpPrefix, "prefix", "", "", "Prefix for the HTTP server routes")

	rootCmd.AddCommand(httpCmd)
}

var httpCmd = &cobra.Command{
	Use:   "http [options] <directory>",
	Short: "Run a Git HTTP server",
	Long:  `Start an HTTP server that serves Git repositories over HTTP.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		directory := args[0]
		addr := fmt.Sprintf(":%d", httpPort)
		abs, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		log.Printf("Using absolute path: %q", abs)
		logger := log.Default()
		loader := transport.NewFilesystemLoader(osfs.New(abs, osfs.WithBoundOS()), false)
		gitmw := githttp.NewBackend(loader)

		handler := serverhttp.LoggingMiddleware(logger, gitmw)
		log.Printf("Starting HTTP server on %q for directory %q", addr, directory)
		if err := http.ListenAndServe(addr, handler); !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		return nil
	},
}

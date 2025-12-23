package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	
	"github.com/xdevplatform/playground/internal/playground"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "playground",
		Short: "X API Playground - Local X API v2 simulator",
		Long: `A standalone local HTTP server that simulates the X (Twitter) API v2
for testing and development without consuming API credits.

The playground provides:
  - Complete API compatibility with X API v2 endpoints
  - Stateful operations with in-memory state management
  - Optional file-based state persistence
  - Interactive web UI for exploring and testing endpoints
  - Server-Sent Events (SSE) streaming support`,
	}

	rootCmd.AddCommand(createStartCmd())
	rootCmd.AddCommand(createStatusCmd())
	rootCmd.AddCommand(createRefreshCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func createStartCmd() *cobra.Command {
	var port int
	var host string
	var refreshCache bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the playground API server",
		Long: `Start a local HTTP server that simulates X API endpoints.
The server runs until interrupted (Ctrl+C).

Access the web UI at http://localhost:8080/playground (or your configured host:port)`,
		Run: func(cmd *cobra.Command, args []string) {
			server := playground.NewServerWithRefresh(port, host, refreshCache)

			// Handle interrupt signals (Ctrl+C, Ctrl+Z, SIGTERM)
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGTSTP)

			// Start server in goroutine
			errChan := make(chan error, 1)
			go func() {
				if err := server.Start(); err != nil && err != http.ErrServerClosed {
					errChan <- err
				}
			}()

			// Wait for interrupt or error
			select {
			case sig := <-sigChan:
				// Handle SIGTSTP (Ctrl+Z) - convert to graceful shutdown
				if sig == syscall.SIGTSTP {
					color.Yellow("\n🛑 Received suspend signal (Ctrl+Z), shutting down gracefully...")
				} else {
					color.Yellow("\n🛑 Received interrupt signal, shutting down...")
				}
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()

				if err := server.Stop(shutdownCtx); err != nil {
					color.Red("Error shutting down server: %v", err)
					os.Exit(1)
				}

				color.Green("✅ Playground server stopped gracefully")
			case err := <-errChan:
				color.Red("❌ Server error: %v", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to run the playground server on")
	cmd.Flags().StringVar(&host, "host", "localhost", "Host to bind the playground server to")
	cmd.Flags().BoolVar(&refreshCache, "refresh", false, "Force refresh of OpenAPI spec cache")

	return cmd
}

func createStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check playground server status",
		Long:  "Check if a playground server is running and display its status",
		Run: func(cmd *cobra.Command, args []string) {
			// Try to connect to common playground ports
			ports := []int{8080, 3000, 8081}
			for _, p := range ports {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", p))
				if err == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						color.Green("✅ Playground server is running on port %d", p)
						return
					}
				}
			}
			color.Red("❌ No playground server found")
		},
	}
}

func createRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh OpenAPI spec cache",
		Long:  "Force refresh of the cached OpenAPI specification",
		Run: func(cmd *cobra.Command, args []string) {
			_, err := playground.LoadOpenAPISpecWithRefresh(true)
			if err != nil {
				color.Red("❌ Failed to refresh OpenAPI spec: %v", err)
				os.Exit(1)
			}
			color.Green("✅ OpenAPI spec cache refreshed")
		},
	}
}


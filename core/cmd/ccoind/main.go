// CCoin Daemon - Main entry point for the CCoin node
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ccoin/core/internal/dag"
	"github.com/ccoin/core/internal/storage"
)

const (
	version = "0.1.0"
	banner  = `
   _____ _____      _       
  / ____/ ____|    (_)      
 | |   | |     ___  _ _ __  
 | |   | |    / _ \| | '_ \ 
 | |___| |___| (_) | | | | |
  \_____\_____\___/|_|_| |_|
                            
  CCoin Daemon v%s
  The Decentralized AI Economy
`
)

// Config holds node configuration
type Config struct {
	// Database
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string

	// Network
	ListenAddr string
	RPCAddr    string

	// Mining
	MinerEnabled bool
	MinerAddress string

	// Logging
	LogLevel string
	LogFile  string

	// Data
	DataDir string
}

func main() {
	// Parse flags
	cfg := parseFlags()

	// Print banner
	fmt.Printf(banner, version)

	// Initialize context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Initialize components
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *Config {
	cfg := &Config{}

	// Database flags
	flag.StringVar(&cfg.DBHost, "db-host", "localhost", "PostgreSQL host")
	flag.IntVar(&cfg.DBPort, "db-port", 5432, "PostgreSQL port")
	flag.StringVar(&cfg.DBUser, "db-user", "ccoin", "PostgreSQL user")
	flag.StringVar(&cfg.DBPassword, "db-password", "", "PostgreSQL password")
	flag.StringVar(&cfg.DBName, "db-name", "ccoin", "PostgreSQL database name")

	// Network flags
	flag.StringVar(&cfg.ListenAddr, "listen", "/ip4/0.0.0.0/tcp/9000", "P2P listen address")
	flag.StringVar(&cfg.RPCAddr, "rpc", "127.0.0.1:9001", "RPC server address")

	// Mining flags
	flag.BoolVar(&cfg.MinerEnabled, "mine", false, "Enable mining")
	flag.StringVar(&cfg.MinerAddress, "miner-address", "", "Miner reward address")

	// Logging flags
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&cfg.LogFile, "log-file", "", "Log file path (empty for stdout)")

	// Data flags
	flag.StringVar(&cfg.DataDir, "data-dir", "./data", "Data directory")

	flag.Parse()

	return cfg
}

func run(ctx context.Context, cfg *Config) error {
	fmt.Println("Initializing CCoin node...")

	// Create data directory
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize database
	fmt.Println("Connecting to database...")
	dbConfig := &storage.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		Database: cfg.DBName,
		SSLMode:  "disable",
		MaxConns: 20,
	}

	store, err := storage.NewPostgresStore(ctx, dbConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer store.Close()
	fmt.Println("Database connected.")

	// Initialize DAG
	fmt.Println("Initializing BlockDAG...")
	blockDAG := dag.NewDAG(store, nil)
	if err := blockDAG.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize DAG: %w", err)
	}
	fmt.Printf("DAG initialized. Height: %d, Tips: %d\n", 
		blockDAG.GetHeight(), len(blockDAG.GetTips()))

	// TODO: Initialize remaining components
	// - P2P Network (libp2p)
	// - Consensus Engine
	// - Mempool
	// - RPC Server
	// - Mining Engine (if enabled)

	fmt.Println("CCoin node started successfully!")
	fmt.Println("Press Ctrl+C to stop.")

	// Wait for shutdown
	<-ctx.Done()

	fmt.Println("Node stopped.")
	return nil
}

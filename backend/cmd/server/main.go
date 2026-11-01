package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"walletledger/internal/ledger"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("PORT must be 1..65535")
	}
	if command == "health" {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + port + "/api/health")
		if err != nil {
			return errors.New("health request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("health endpoint is not ready")
		}
		fmt.Println("ready")
		return nil
	}
	if command != "serve" && command != "migrate" {
		return errors.New("usage: walletledger [serve|migrate|health]")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return errors.New("DATABASE_URL is invalid")
	}
	cfg.MaxConns = 8
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 2 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "12000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "8000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "15000"
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("database pool could not be initialized")
	}
	defer pool.Close()
	if command == "migrate" {
		migrationContext, done := context.WithTimeout(ctx, 60*time.Second)
		defer done()
		if err = ledger.Migrate(migrationContext, pool); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		fmt.Println("schema version 1 ready")
		return nil
	}
	var origins []string
	for _, origin := range strings.Split(os.Getenv("WALLETLEDGER_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	secure := os.Getenv("WALLETLEDGER_COOKIE_SECURE") == "true"
	receiptSecret := os.Getenv("WALLETLEDGER_RECEIPT_SECRET")
	if len(receiptSecret) < 32 {
		return errors.New("WALLETLEDGER_RECEIPT_SECRET must contain at least 32 bytes and remain stable across restarts")
	}
	api := ledger.New(pool, ledger.Config{AllowedOrigins: origins, CookieSecure: secure, SessionHours: 12, ReceiptSecret: receiptSecret})
	host := os.Getenv("WALLETLEDGER_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	server := &http.Server{Addr: net.JoinHostPort(host, port), Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return errors.New("HTTP listener could not be opened")
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	log.Printf("WalletLedger listening on %s", server.Addr)
	select {
	case err = <-finished:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server stopped unexpectedly")
		}
	case <-ctx.Done():
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err = server.Shutdown(shutdown); err != nil {
			return errors.New("HTTP shutdown timed out")
		}
	}
	return nil
}

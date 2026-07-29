package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"rosboard/internal/api"
	"rosboard/internal/auth"
	"rosboard/internal/config"
	"rosboard/internal/service"
	"rosboard/internal/store"
	"rosboard/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		if err := runAdminCommand(os.Args[2:], terminalPasswordReader(os.Stdin, os.Stderr), auth.New); err != nil {
			log.Fatalf("admin command: %v", err)
		}
		return
	}

	configPath := flag.String("config", "", "Path to YAML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	storage, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer storage.Close()

	assets, err := ui.Assets()
	if err != nil {
		log.Fatalf("load UI assets: %v", err)
	}

	logger := log.New(os.Stdout, "rosboard ", log.LstdFlags)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var manager *service.MonitorManager
	if cfg.RouterOSConfigured() {
		manager = service.NewMonitorManager(cfg, storage, logger)
		go manager.Start(ctx)
	} else {
		logger.Print("routeros is not configured, serving setup UI")
	}

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           api.NewServerWithManager(cfg, manager, storage, assets, func() { os.Exit(0) }),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Printf("serving on %s using data dir %s", cfg.ListenAddress, filepath.Clean(cfg.DataDir))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}

type passwordReader func(prompt string) (string, error)

func terminalPasswordReader(input *os.File, output io.Writer) passwordReader {
	return func(prompt string) (string, error) {
		if !term.IsTerminal(int(input.Fd())) {
			return "", errors.New("password reset requires an interactive terminal")
		}
		if _, err := fmt.Fprint(output, prompt); err != nil {
			return "", err
		}
		payload, err := term.ReadPassword(int(input.Fd()))
		_, _ = fmt.Fprintln(output)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(payload), nil
	}
}

func runAdminCommand(args []string, readPassword passwordReader, serviceFactory func(*store.Store) *auth.Service) error {
	if len(args) == 0 || args[0] != "reset-password" {
		return errors.New("usage: rosboard admin reset-password -config <path>")
	}
	flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Path to YAML config file")
	if err := flags.Parse(args[1:]); err != nil {
		return errors.New("usage: rosboard admin reset-password -config <path>")
	}
	if flags.NArg() != 0 || strings.TrimSpace(*configPath) == "" {
		return errors.New("usage: rosboard admin reset-password -config <path>")
	}
	if _, err := os.Stat(*configPath); err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	storage, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer storage.Close()
	password, err := readPassword("New administrator password: ")
	if err != nil {
		return err
	}
	confirmation, err := readPassword("Confirm new password: ")
	if err != nil {
		return err
	}
	if err := serviceFactory(storage).ResetPassword(context.Background(), password, confirmation); err != nil {
		return err
	}
	fmt.Println("Administrator password reset; all sessions were revoked.")
	return nil
}

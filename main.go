package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.json", "optional path to the JSON config file")
	once := flag.Bool("once", false, "run one update check and exit")
	flag.Parse()

	if err := run(*configPath, *once); err != nil {
		log.Fatal(err)
	}
}

func run(configPath string, once bool) error {
	config, interval, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	apiHTTPClient := &http.Client{Timeout: 15 * time.Second}
	ipHTTPClient := newIPv4HTTPClient(15 * time.Second)
	cloudflare := newCloudflareClient(apiHTTPClient, config)
	ddns := newUpdater(ipHTTPClient, cloudflare, config)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	syncOnce := func() error {
		result, err := ddns.sync(ctx)
		if err != nil {
			return err
		}
		switch result.Action {
		case "created":
			log.Printf("created A record %s -> %s", config.Domain, result.IP)
		case "updated":
			log.Printf("updated A record %s -> %s", config.Domain, result.IP)
		case "unchanged":
			log.Printf("A record %s is already %s", config.Domain, result.IP)
		default:
			return fmt.Errorf("unexpected sync result %q", result.Action)
		}
		return nil
	}

	if once {
		return syncOnce()
	}

	if err := syncOnce(); err != nil {
		log.Printf("update failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Print("stopped")
			return nil
		case <-ticker.C:
			if err := syncOnce(); err != nil {
				log.Printf("update failed: %v", err)
			}
		}
	}
}

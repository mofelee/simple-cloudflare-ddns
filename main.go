package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
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

	runContinuously(ctx, interval, syncOnce)
	log.Print("stopped")
	return nil
}

const initialRetryDelay = 10 * time.Second

func runContinuously(ctx context.Context, interval time.Duration, syncOnce func() error) {
	consecutiveFailures := 0
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			err := syncOnce()
			if ctx.Err() != nil {
				return
			}

			nextDelay := interval
			if err != nil {
				consecutiveFailures++
				nextDelay = retryDelay(interval, consecutiveFailures, rand.Float64())
				log.Printf("update failed: %v; retrying in %s", err, nextDelay.Round(time.Second))
			} else {
				consecutiveFailures = 0
			}
			timer.Reset(nextDelay)
		}
	}
}

func retryDelay(interval time.Duration, consecutiveFailures int, randomValue float64) time.Duration {
	if interval <= initialRetryDelay {
		return interval
	}

	delay := initialRetryDelay
	for failure := 1; failure < consecutiveFailures; failure++ {
		if delay >= interval/2 {
			delay = interval
			break
		}
		delay *= 2
	}
	if delay >= interval {
		return interval
	}

	if randomValue < 0 {
		randomValue = 0
	} else if randomValue > 1 {
		randomValue = 1
	}
	jittered := time.Duration(float64(delay) * (0.8 + 0.4*randomValue))
	if jittered > interval {
		return interval
	}
	return jittered
}

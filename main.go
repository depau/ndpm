// Command ndpm polls GitHub releases for Navidrome
// plugins and installs them into a folder that Navidrome watches and
// auto-reloads from.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/depau/ndpm/config"
	"github.com/depau/ndpm/internal/github"
	"github.com/depau/ndpm/internal/manager"
)

func main() {
	var cfgPath string
	var once bool
	var interval string
	flag.StringVar(&cfgPath, "config", "config.yaml", "path to YAML config")
	flag.BoolVar(&once, "once", false, "run a single sync and exit")
	flag.StringVar(&interval, "interval", "", "overrides the config interval")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gh, err := github.New()
	if err != nil {
		log.Fatalf("github: %v", err)
	}
	if cfg.GitHubToken != "" {
		gh.SetToken(cfg.GitHubToken)
	}

	stage := manager.NewStaging(cfg.PluginsDir)
	if err := os.MkdirAll(cfg.PluginsDir, 0o755); err != nil {
		log.Fatalf("creating plugins dir: %v", err)
	}
	updater := manager.NewUpdater(gh, stage)

	for {
		changed, err := syncAll(updater, cfg.Sources)
		if err != nil {
			log.Printf("sync: %v", err)
		}
		if once {
			if err != nil {
				os.Exit(1)
			}
			if changed == 0 {
				log.Printf("nothing to update")
			}
			return
		}
		wait := cfg.Interval
		if interval != "" {
			wait = interval
		}
		// Respect GitHub's rate-limit reset: if throttled, wait out the
		// window (or the interval, whichever's shorter) instead of hammering.
		if until := gh.ThrottledUntil(); until.After(time.Now()) {
			d := time.Until(until)
			if maxD, err := time.ParseDuration(wait); err == nil && d > maxD {
				d = maxD
			}
			log.Printf("GitHub rate limit is active; backing off for %s", d.Round(time.Second))
			time.Sleep(d)
			continue
		}
		sleep(wait)
	}
}

// syncAll installs any new releases and returns how many changed.
func syncAll(updater *manager.Updater, sources []config.Source) (int, error) {
	var lastErr error
	changed := 0
	for _, src := range sources {
		p, err := updater.Install(src)
		switch {
		case errors.Is(err, manager.ErrUpToDate):
			log.Printf("up to date: %s @ %s", src.Repo, p.Version)
		case err != nil:
			log.Printf("error %s: %v", src.Repo, err)
			lastErr = err
		default:
			log.Printf("installed %s -> %s.ndp (%s)", src.Repo, p.Slug, p.Version)
			changed++
		}
	}
	return changed, lastErr
}

// sleep waits for the given duration string like "1h30m". Unknown values
// fall back to the default interval.
func sleep(interval string) {
	const fallback = time.Hour
	d, err := time.ParseDuration(interval)
	if err != nil || d <= 0 {
		log.Printf("invalid interval %q, using %s", interval, fallback)
		d = fallback
	}
	time.Sleep(d)
}

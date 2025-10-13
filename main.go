package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/henrywhitaker3/adguard-exporter/internal/adguard"
	"github.com/henrywhitaker3/adguard-exporter/internal/config"
	"github.com/henrywhitaker3/adguard-exporter/internal/http"
	"github.com/henrywhitaker3/adguard-exporter/internal/metrics"
	"github.com/henrywhitaker3/adguard-exporter/internal/worker"
)

func main() {
	// Command line flags
	listCategories := flag.Bool("list-categories", false, "List all available DNS categories and exit")
	printQueries := flag.Bool("print-queries", false, "Print query log details to console instead of running exporter")
	flag.Parse()

	// Handle list-categories flag
	if *listCategories {
		categories := worker.GetAvailableCategories()
		sort.Strings(categories)
		fmt.Printf("Available DNS categories (%d total):\n\n", len(categories))
		for _, cat := range categories {
			fmt.Printf("  - %s\n", cat)
		}
		os.Exit(0)
	}

	// Handle print-queries flag
	if *printQueries {
		global, err := config.FromEnv()
		if err != nil {
			panic(err)
		}

		clients := []*adguard.Client{}
		for _, conf := range global.Configs {
			clients = append(clients, adguard.NewClient(conf))
		}

		ctx := context.Background()

		// Print query logs for each client
		for _, client := range clients {
			if err := worker.PrintQueryLogStats(ctx, client); err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching query log for %s: %v\n", client.Url(), err)
				os.Exit(1)
			}
		}

		os.Exit(0)
	}

	metrics.Init()
	global, err := config.FromEnv()
	if err != nil {
		panic(err)
	}

	clients := []*adguard.Client{}
	for _, conf := range global.Configs {
		clients = append(clients, adguard.NewClient(conf))
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	http := http.NewHttp(global.Server.Debug)
	go http.Serve(global.Server.BindAddr)
	go worker.Work(ctx, global.Server.Interval, clients)
	http.Ready(true)
	http.Healthy(true)

	<-sigs
	if err := http.Stop(ctx); err != nil {
		panic(err)
	}
}

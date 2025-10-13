package worker

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/henrywhitaker3/adguard-exporter/internal/adguard"
	"github.com/henrywhitaker3/adguard-exporter/internal/metrics"
	"github.com/henrywhitaker3/adguard-exporter/internal/state"
	"golang.org/x/net/publicsuffix"
)

var (
	initialised  = []string{}
	versions     = map[string]string{}
	workerState  *state.State
	stateInitErr error
)

func init() {
	// Initialize state with default path
	stateFilePath := ".cache/adguard-exporter/state.json"
	workerState, stateInitErr = state.New(stateFilePath)
	if stateInitErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING - could not initialize state file: %v (will process all records)\n", stateInitErr)
	}
}

func Work(ctx context.Context, interval time.Duration, clients []*adguard.Client) {
	log.Printf("Collecting metrics every %s\n", interval)
	tick := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, c := range clients {
				go collect(ctx, c)
			}
		}
	}
}

func collect(ctx context.Context, client *adguard.Client) error {
	// Initialise the scrape errors counter with a 0
	if !slices.Contains(initialised, client.Url()) {
		metrics.ScrapeErrors.WithLabelValues(client.Url())
		initialised = append(initialised, client.Url())
	}

	go collectStats(ctx, client)
	go collectStatus(ctx, client)
	go collectDhcp(ctx, client)
	go collectQueryLogStats(ctx, client)

	return nil
}

func collectStats(ctx context.Context, client *adguard.Client) {
	stats, err := client.GetStats(ctx)
	if err != nil {
		log.Printf("ERROR - could not get stats: %v\n", err)
		metrics.ScrapeErrors.WithLabelValues(client.Url()).Inc()
		return
	}

	names := map[string]string{}
	if len(stats.TopClients) > 0 {
		names, err = client.SearchClients(ctx, stats.TopClients)
		if err != nil {
			log.Printf("ERROR - could not search clients: %v\n", err)
			metrics.ScrapeErrors.WithLabelValues(client.Url()).Inc()
		}
	}

	metrics.TotalQueries.WithLabelValues(client.Url()).Set(float64(stats.TotalQueries))
	metrics.BlockedFiltered.WithLabelValues(client.Url()).Set(float64(stats.BlockedFilteredQueries))
	metrics.ReplacedSafesearch.WithLabelValues(client.Url()).Set(float64(stats.ReplacedSafesearchQueries))
	metrics.ReplacedSafebrowsing.WithLabelValues(client.Url()).Set(float64(stats.ReplacedSafebrowsingQueries))
	metrics.ReplacedParental.WithLabelValues(client.Url()).Set(float64(stats.ReplacedParentalQueries))
	metrics.AvgProcessingTime.WithLabelValues(client.Url()).Set(float64(stats.AvgProcessingTime))

	for _, c := range stats.TopClients {
		for key, val := range c {
			name := names[key]
			if name == "" {
				name = key // fallback to IP if no name
			}
			metrics.TopClients.WithLabelValues(client.Url(), key, name).Set(float64(val))
		}
	}
	for _, c := range stats.TopUpstreamsResponses {
		for key, val := range c {
			metrics.TopUpstreams.WithLabelValues(client.Url(), key).Set(float64(val))
		}
	}
	for _, c := range stats.TopQueriedDomains {
		for key, val := range c {
			metrics.TopQueriedDomains.WithLabelValues(client.Url(), key).Set(float64(val))
		}
	}
	for _, c := range stats.TopBlockedDomains {
		for key, val := range c {
			metrics.TopBlockedDomains.WithLabelValues(client.Url(), key).Set(float64(val))
		}
	}
	for _, c := range stats.TopUpstreamsAvgTimes {
		for key, val := range c {
			metrics.TopUpstreamsAvgTimes.WithLabelValues(client.Url(), key).Set(float64(val))
		}
	}
}

func collectStatus(ctx context.Context, client *adguard.Client) {
	status, err := client.GetStatus(ctx)
	if err != nil {
		log.Printf("ERROR - could not get status: %v\n", err)
		metrics.ScrapeErrors.WithLabelValues(client.Url()).Inc()
		return
	}
	// Persist the running version the first time
	if _, ok := versions[client.Url()]; !ok {
		versions[client.Url()] = status.Version
	}

	// Check if the adguard version has changed
	if versions[client.Url()] != status.Version {
		metrics.Running.Reset()
	}

	metrics.Running.WithLabelValues(client.Url(), status.Version).Set(float64(status.Running.Int()))
	metrics.ProtectionEnabled.WithLabelValues(client.Url()).Set(float64(status.ProtectionEnabled.Int()))
}

func collectDhcp(ctx context.Context, client *adguard.Client) {
	dhcp, err := client.GetDhcp(ctx)
	if err != nil {
		log.Printf("ERROR - could not get dhcp status: %v\n", err)
		metrics.ScrapeErrors.WithLabelValues(client.Url()).Inc()
		return
	}
	metrics.DhcpEnabled.WithLabelValues(client.Url()).Set(float64(dhcp.Enabled.Int()))
	metrics.DhcpLeases.Record(client.Url(), dhcp.Leases)
}

// getETLDPlusOne extracts the eTLD+1 (effective top-level domain + 1) from a domain name.
// For example: "www.example.com" -> "example.com", "api.github.com" -> "github.com"
// Returns the original domain if extraction fails or for invalid domains.
func getETLDPlusOne(domain string) string {
	// Remove trailing dot if present
	domain = strings.TrimSuffix(domain, ".")

	// Return empty string as-is
	if domain == "" {
		return domain
	}

	// Check if this is an IP address - if so, return as-is
	if net.ParseIP(domain) != nil {
		return domain
	}

	// Extract eTLD+1
	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		// If extraction fails (e.g., for invalid domains), return the original
		return domain
	}

	return etldPlusOne
}

func collectQueryLogStats(ctx context.Context, client *adguard.Client) {
	// Get last query time from state (0 if none exists)
	var lastQueryEpoch int64
	if workerState != nil {
		lastQueryEpoch = workerState.GetLastQueryTime(client.Url())
	}

	stats, times, queries, latestEpoch, err := client.GetQueryLog(ctx, lastQueryEpoch)
	if err != nil {
		log.Printf("ERROR - could not get query type stats: %v\n", err)
		metrics.ScrapeErrors.WithLabelValues(client.Url()).Inc()
		return
	}

	// Update state with the latest timestamp if we got any records
	if latestEpoch > 0 && workerState != nil {
		if err := workerState.SetLastQueryTime(client.Url(), latestEpoch); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING - could not save last query time: %v\n", err)
		}
	}

	for c, v := range stats {
		for t, v := range v {
			metrics.QueryTypes.WithLabelValues(client.Url(), t, c).Set(float64(v))
		}
	}

	for _, l := range queries {
		elapsed, err := strconv.ParseFloat(l.Elapsed, 64)
		if err != nil {
			continue
		}
		protocol := l.ClientProto
		if protocol == "" {
			protocol = "plain"
		}
		etldDomain := getETLDPlusOne(l.Question.Host)
		categories := CategorizeDomain(l.Question.Host)
		queryType := l.Question.Type
		if queryType == "" {
			queryType = "unknown"
		}

		// Join categories into comma-separated string, use "unknown" if empty
		categoryLabel := strings.Join(categories, ",")
		if categoryLabel == "" {
			categoryLabel = "unknown"
		}

		metrics.TotalQueriesDetails.WithLabelValues(client.Url(), l.Client, l.Reason, l.Status, l.Upstream, l.ClientInfo.Name, protocol, etldDomain, categoryLabel, queryType).Set(elapsed)
		metrics.TotalQueriesDetailsHistogram.WithLabelValues(client.Url(), l.Client, l.Reason, l.Status, l.Upstream, l.ClientInfo.Name, protocol, etldDomain, categoryLabel, queryType).Observe(float64(elapsed))
	}

    log.Printf("Retrieved: %d records", len(queries))

	for _, t := range times {
		metrics.ProcessingTimeBucketMilli.
			WithLabelValues(client.Url(), t.Client, t.Upstream).
			Observe(float64(t.Elapsed.Milliseconds()))
		metrics.ProcessingTimeBucket.
			WithLabelValues(client.Url(), t.Client, t.Upstream).
			Observe(t.Elapsed.Seconds())
	}
}

// PrintQueryLogStats fetches and prints query log details to console instead of recording to Prometheus
// This is useful for debugging and understanding what data would be collected
// Informational messages go to stderr, record data goes to stdout
func PrintQueryLogStats(ctx context.Context, client *adguard.Client) error {
	// Get last query time from state (0 if none exists)
	var lastQueryEpoch int64
	if workerState != nil {
		lastQueryEpoch = workerState.GetLastQueryTime(client.Url())
	}

	stats, times, queries, latestEpoch, err := client.GetQueryLog(ctx, lastQueryEpoch)
	if err != nil {
		return fmt.Errorf("could not get query log: %w", err)
	}

	log.Printf("# %s", client.Url())
	log.Printf("  - %d Records between: %s ... %s", len(queries), time.UnixMilli(lastQueryEpoch).Format(time.RFC3339Nano), time.UnixMilli(latestEpoch).Format(time.RFC3339Nano))

	// Print query type stats
	if len(stats) > 0 {
		log.Printf("## Query Types by Client")
        fmt.Printf("%-15s\t%-10s\t%s\n", "Client", "Type", "Count")
		for clientIP, types := range stats {
			for queryType, count := range types {
				fmt.Printf("%-15s\t%-10s\t%d\n", clientIP, queryType, count)
			}
		}
	}

	// Print query details (like TotalQueriesDetails metric) - records go to stdout
	if len(queries) > 0 {
		log.Printf("## Details")

		fmt.Printf("%-15s\t%-15s\t%-20s\t%-10s\t%-15s\t%-30s\t%-15s\t%-10s\t%-25s\t%-10s\t%s\n",
			"Server", "Client", "ClientName", "Reason", "Status", "Upstream", "Protocol", "QueryType", "eTLD+1", "Category", "Elapsed(ms)")

		// Header to stdout
		fmt.Printf("%-15s %-15s %-20s %-10s %-15s %-30s %-15s %-10s %-25s %-10s %s\n",
			"Server", "Client", "ClientName", "Reason", "Status", "Upstream", "Protocol", "QueryType", "eTLD+1", "Category", "Elapsed(ms)")
		fmt.Println(strings.Repeat("-", 180))

		// Records to stdout
		for _, l := range queries {
			elapsed, err := strconv.ParseFloat(l.Elapsed, 64)
			if err != nil {
				continue
			}

			protocol := l.ClientProto
			if protocol == "" {
				protocol = "plain"
			}

			etldDomain := getETLDPlusOne(l.Question.Host)
			categories := CategorizeDomain(l.Question.Host)
			queryType := l.Question.Type
			if queryType == "" {
				queryType = "unknown"
			}

			categoryLabel := strings.Join(categories, ",")
			if categoryLabel == "" {
				categoryLabel = "unknown"
			}

			// Truncate long fields for better display
			clientName := l.ClientInfo.Name
			if len(clientName) > 18 {
				clientName = clientName[:15] + "..."
			}
			upstream := l.Upstream
			if len(upstream) > 28 {
				upstream = upstream[:25] + "..."
			}
			etld := etldDomain
			if len(etld) > 23 {
				etld = etld[:20] + "..."
			}

			fmt.Printf("%-15s %-15s %-20s %-10s %-15s %-30s %-15s %-10s %-25s %-10s %.2f\n",
				client.Url(), l.Client, clientName, l.Reason, l.Status, upstream, protocol, queryType, etld, categoryLabel, elapsed)
		}
		fmt.Println()
	}

	// Print processing times summary to stderr
	if len(times) > 0 {
		log.Printf("## Processing Times Summary")
		log.Printf("  - Total timing entries: %d", len(times))
		var totalTime time.Duration
		for _, t := range times {
			totalTime += t.Elapsed
		}
		avgTime := totalTime / time.Duration(len(times))
		log.Printf("  - Average processing time: %v\n", avgTime)
	}

	// Update state with the latest timestamp if we got any records
	if latestEpoch > 0 && workerState != nil {
		if err := workerState.SetLastQueryTime(client.Url(), latestEpoch); err != nil {
			return fmt.Errorf("could not save last query time: %w", err)
		}
		log.Printf("  - Updated latest query time: %s", time.UnixMilli(latestEpoch).Format(time.RFC3339Nano))
	}

	return nil
}

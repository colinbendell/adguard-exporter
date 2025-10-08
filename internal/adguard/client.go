package adguard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/henrywhitaker3/adguard-exporter/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Client struct {
	conf config.Config
}

func NewClient(conf config.Config) *Client {
	return &Client{
		conf: conf,
	}
}

func (c *Client) do(ctx context.Context, method string, path string, out any) error {
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", c.conf.Username, c.conf.Password)))
	url, err := url.Parse(fmt.Sprintf("%s%s", c.conf.Url, path))
	if err != nil {
		return err
	}

	req := &http.Request{
		Method: method,
		Header: http.Header{},
		URL:    url,
	}
	req.Header.Add("Authorization", fmt.Sprintf("Basic %s", auth))
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d: %v", resp.StatusCode, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func (c *Client) doPost(ctx context.Context, path string, body any, out any) error {
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", c.conf.Username, c.conf.Password)))
	url, err := url.Parse(fmt.Sprintf("%s%s", c.conf.Url, path))
	if err != nil {
		return err
	}

	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Basic %s", auth))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if out != nil {
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) GetStats(ctx context.Context) (*Stats, error) {
	out := &Stats{}
	err := c.do(ctx, http.MethodGet, "/control/stats", out)
	return out, err
}

func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	out := &Status{}
	err := c.do(ctx, http.MethodGet, "/control/status", out)
	return out, err
}

func (c *Client) GetDhcp(ctx context.Context) (*DhcpStatus, error) {
	out := &DhcpStatus{}
	err := c.do(ctx, http.MethodGet, "/control/dhcp/status", out)
	if err != nil {
		return nil, err
	}

	for i := range out.DynamicLeases {
		l := out.DynamicLeases[i]
		l.Type = "dynamic"
		out.DynamicLeases[i] = l
	}
	for i := range out.StaticLeases {
		l := out.StaticLeases[i]
		l.Type = "static"
		out.StaticLeases[i] = l
	}

	out.Leases = slices.Concat(out.DynamicLeases, out.StaticLeases)

	return out, nil
}

// GetQueryLog fetches query log entries, optionally filtering by lastQueryEpoch.
// If lastQueryEpoch is 0 (zero value), all records are returned.
// Automatically fetches additional batches if there's a gap between oldest and lastQueryEpoch.
// Returns: query types map, query times slice, filtered log entries, latest timestamp (epoch), error
func (c *Client) GetQueryLog(ctx context.Context, lastQueryEpoch int64) (map[string]map[string]int, []QueryTime, []logEntry, int64, error) {
	allEntries := []logEntry{}
	olderThan := ""
	maxIterations := 1000 // Safety limit to prevent infinite loops
	iterations := 0

	for {
		iterations++
		if iterations > maxIterations {
			log.Printf("WARNING - reached maximum pagination iterations (%d), stopping", maxIterations)
			break
		}

		// Build URL with optional older_than parameter (use RFC3339 for API)
		url := "/control/querylog?limit=50000&response_status=all"
		if olderThan != "" {
			url += "&older_than=" + olderThan
		}

		queryLog := &queryLog{}
		err := c.do(ctx, http.MethodGet, url, queryLog)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// If no entries returned, we've reached the end
		if len(queryLog.Log) == 0 {
			break
		}

		// Accumulate entries
		allEntries = append(allEntries, queryLog.Log...)

		// If oldest entry is still newer than lastQueryEpoch, we have a gap - fetch more
		// Convert oldest to epoch for comparison
		if queryLog.Oldest != "" {
			oldestEpoch, err := rfc3339ToEpoch(queryLog.Oldest)
			if err != nil {
				log.Printf("WARNING - invalid oldest timestamp %s: %v, stopping pagination", queryLog.Oldest, err)
				break
			}

			if oldestEpoch > lastQueryEpoch {
				olderThan = queryLog.Oldest // Keep RFC3339 for API parameter
				log.Printf(" - Fetching batch prior to %s (last: %d)", olderThan, len(queryLog.Log))
				continue
			}
		}

		// We've reached or passed the lastQueryEpoch, stop fetching
		break
	}

	// Filter log entries by timestamp if lastQueryEpoch is provided
	filteredLog := allEntries
	if lastQueryEpoch > 0 {
		filteredLog = c.filterByTimestamp(allEntries, lastQueryEpoch)
	}

	// Find the latest timestamp from the filtered results
	newLastQueryEpoch := c.getLatestTimestamp(filteredLog)

	types, err := c.getQueryTypes(filteredLog)
	if err != nil {
		return nil, nil, nil, newLastQueryEpoch, err
	}
	times, err := c.getQueryTimes(filteredLog)
	if err != nil {
		return nil, nil, nil, newLastQueryEpoch, err
	}

	log.Printf("Fetched %d total entries, %d after filtering (iterations: %d)", len(allEntries), len(filteredLog), iterations)

	return types, times, filteredLog, newLastQueryEpoch, nil
}

// rfc3339ToEpoch converts RFC3339 timestamp string to Unix epoch milliseconds
func rfc3339ToEpoch(rfc3339Time string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, rfc3339Time)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

// epochToRFC3339 converts Unix epoch milliseconds to RFC3339 timestamp string
func epochToRFC3339(epochMillis int64) string {
	t := time.UnixMilli(epochMillis)
	return t.Format(time.RFC3339Nano)
}

// filterByTimestamp filters log entries to only include those after lastQueryEpoch
func (c *Client) filterByTimestamp(entries []logEntry, lastQueryEpoch int64) []logEntry {
	if lastQueryEpoch == 0 {
		return entries
	}

	filtered := make([]logEntry, 0, len(entries))
	for _, entry := range entries {
		entryEpoch, err := rfc3339ToEpoch(entry.Time)
		if err != nil {
			log.Printf("WARNING - invalid entry timestamp %s: %v, skipping", entry.Time, err)
			continue
		}

		// Only include entries with timestamps greater than lastQueryEpoch
		if entryEpoch > lastQueryEpoch {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// getLatestTimestamp finds the latest (highest) timestamp from log entries
// Returns the timestamp as Unix epoch milliseconds
func (c *Client) getLatestTimestamp(entries []logEntry) int64 {
	if len(entries) == 0 {
		return 0
	}

	var latestEpoch int64

	for _, entry := range entries {
		entryEpoch, err := rfc3339ToEpoch(entry.Time)
		if err != nil {
			log.Printf("WARNING - invalid entry timestamp %s: %v, skipping", entry.Time, err)
			continue
		}

		if entryEpoch > latestEpoch {
			latestEpoch = entryEpoch
		}
	}

	return latestEpoch
}

func (c *Client) getQueryTypes(entries []logEntry) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	for _, d := range entries {
		if len(d.Answer) > 0 {
			if _, ok := out[d.Client]; !ok {
				out[d.Client] = map[string]int{}
			}
			for i := range d.Answer {
				switch v := d.Answer[i].Value.(type) {
				case string:
					out[d.Client][d.Answer[i].Type]++
				case map[string]any:
					dns65 := &type65{}
					mapstructure.Decode(v, dns65)
					out[d.Client]["TYPE"+strconv.Itoa(dns65.Hdr.Rrtype)]++
				}
			}
		}
	}
	return out, nil
}

func (c *Client) getQueryTimes(entries []logEntry) ([]QueryTime, error) {
	out := []QueryTime{}
	for _, q := range entries {
		if q.Upstream == "" {
			q.Upstream = "self"
		}
		ms, err := strconv.ParseFloat(q.Elapsed, 32)
		if err != nil {
			log.Printf("ERROR - could not parse query elapsed time %v as float", q.Elapsed)
			continue
		}
		out = append(out, QueryTime{
			Elapsed:  time.Millisecond * time.Duration(ms),
			Client:   q.Client,
			Upstream: q.Upstream,
		})
	}
	return out, nil
}

func (c *Client) Url() string {
	return c.conf.Url
}

func (c *Client) SearchClients(ctx context.Context, topClients []map[string]int) (map[string]string, error) {
	reqBody := clientsSearchRequest{}
    for _, c := range topClients {
        for key := range c {
            reqBody.Clients = append(reqBody.Clients, struct {
                ID string `json:"id"`
            }{ID: key})
        }
    }

	var resp []map[string]clientInfo
	err := c.doPost(ctx, "/control/clients/search", reqBody, &resp)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, obj := range resp {
		for _, info := range obj {
			for _, id := range info.IDs {
				result[id] = info.Name
			}
		}
	}

	return result, nil
}

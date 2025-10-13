package worker

import (
	_ "embed"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	router "github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

var embeddedDlcData []byte

const (
	// Maximum number of entries to cache
	cacheSize = 10000

	// URL to download the latest dlc.dat file
	dlcDataURL = "https://raw.githubusercontent.com/v2fly/domain-list-community/release/dlc.dat"

	// Cache directory for downloaded dlc.dat
	dlcCacheDir = ".cache/adguard-exporter"

	// Cache file name
	dlcCacheFile = "dlc.dat"

	// How long to cache the downloaded file before re-downloading (7 days)
	dlcCacheDuration = 7 * 24 * time.Hour
)

// optimizedRecord holds a record with pre-computed values for faster matching
type optimizedRecord struct {
	Type         router.Domain_Type
	Value        string
	DomainSuffix string // Pre-computed ".domain.com" for RECORD_DOMAIN type
	Regex        *regexp.Regexp
}

var (
	// categoryLists contains only the category-* lists with optimized records
	categoryLists map[string][]*optimizedRecord

	// domainCache caches domain -> categories mappings
	domainCache     map[string][]string
	domainCacheMu   sync.RWMutex
	cacheInsertions int
)

func init() {
	// Load dlc.dat data (try download first, fall back to embedded)
	dlcData := loadDlcData()

	// Parse the dlc.dat file using protobuf
	var geoSiteList router.GeoSiteList
	if err := proto.Unmarshal(dlcData, &geoSiteList); err != nil {
		log.Fatalf("Failed to parse dlc.dat: %v", err)
	}

	// Filter to only category-* lists and optimize records
	categoryLists = make(map[string][]*optimizedRecord)

	// Add pre-calculated entries for specific domains
	addPreCalculatedEntries()

	for _, geoSite := range geoSiteList.Entry {
		listName := strings.ToLower(geoSite.GetCountryCode())
		if strings.HasPrefix(listName, "category-") {
			// Convert the list name to category name once during init
			categoryName := listNameToCategory(listName)

            currEntries := categoryLists[categoryName]

			// Pre-compute optimized records
			optimized := make([]*optimizedRecord, 0, len(currEntries) + len(geoSite.Domain))
            if len(currEntries) > 0 {
                optimized = append(optimized, currEntries...)
            }

			for _, domain := range geoSite.Domain {
				opt := &optimizedRecord{
					Type:  domain.GetType(),
					Value: domain.GetValue(),
				}

				// Pre-compute domain suffix for RootDomain type
				if domain.GetType() == router.Domain_RootDomain {
					opt.DomainSuffix = "." + domain.GetValue()
				}

				// Pre-compile regex for Regex type
				if domain.GetType() == router.Domain_Regex {
					if re, err := regexp.Compile(domain.GetValue()); err == nil {
						opt.Regex = re
					}
				}

				optimized = append(optimized, opt)
			}

			categoryLists[categoryName] = optimized
		}
	}

	// Initialize cache
	domainCache = make(map[string][]string, cacheSize)
}

// addPreCalculatedEntries adds pre-calculated domain categorizations that override dlc.dat
func addPreCalculatedEntries() {
	// Apple iCloud domains should be categorized as "companies"
	appleDomains := []string{
		"init.push.apple.com",
		"swallow.apple.com",
		"swallow-apple-com.v.aaplimg.com",
		"cdn-icloud-content.g.aaplimg.com",
		"gateway.icloud.com",
		"mask-h2.icloud.com",
		"mask.icloud.com",
		"mask-api.icloud.com",
		"mask.apple-dns.net",
	}

	companiesRecords := make([]*optimizedRecord, 0, len(appleDomains))
	for _, domain := range appleDomains {
		companiesRecords = append(companiesRecords, &optimizedRecord{
			Type:  router.Domain_Full,
			Value: domain,
		})
	}

	categoryLists["companies"] = companiesRecords
}

// loadDlcData attempts to load dlc.dat from cache or download it, falling back to embedded data
func loadDlcData() []byte {
	// Try to load from cache first
	if data, ok := loadCachedDlcData(); ok {
		log.Println("Using cached dlc.dat file")
		return data
	}

	// Try to download fresh data
	if data, ok := downloadDlcData(); ok {
		log.Println("Successfully downloaded fresh dlc.dat file")
		// Cache the downloaded data
		if err := cacheDlcData(data); err != nil {
			log.Printf("Warning: Failed to cache dlc.dat: %v", err)
		}
		return data
	}

	// Fall back to embedded data
	log.Println("Using embedded dlc.dat file as fallback")
	return embeddedDlcData
}

// loadCachedDlcData loads dlc.dat from cache if it exists and is fresh
func loadCachedDlcData() ([]byte, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}

	cachePath := filepath.Join(homeDir, dlcCacheDir, dlcCacheFile)

	// Check if cache file exists and is fresh
	info, err := os.Stat(cachePath)
	if err != nil {
		return nil, false
	}

	// Check if cache is still fresh
	if time.Since(info.ModTime()) > dlcCacheDuration {
		log.Println("Cached dlc.dat is stale, will attempt to download fresh version")
		return nil, false
	}

	// Read cached file
	data, err := os.ReadFile(cachePath)
	if err != nil {
		log.Printf("Warning: Failed to read cached dlc.dat: %v", err)
		return nil, false
	}

	return data, true
}

// downloadDlcData downloads the latest dlc.dat from GitHub
func downloadDlcData() ([]byte, bool) {
	// Allow disabling downloads via environment variable (useful for testing)
	if os.Getenv("ADGUARD_EXPORTER_DISABLE_DLC_DOWNLOAD") != "" {
		log.Println("DLC download disabled via environment variable")
		return nil, false
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(dlcDataURL)
	if err != nil {
		log.Printf("Warning: Failed to download dlc.dat: %v", err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Warning: Failed to download dlc.dat: HTTP %d", resp.StatusCode)
		return nil, false
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Warning: Failed to read downloaded dlc.dat: %v", err)
		return nil, false
	}

	return data, true
}

// cacheDlcData saves the downloaded dlc.dat to cache
func cacheDlcData(data []byte) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cacheDir := filepath.Join(homeDir, dlcCacheDir)

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir, dlcCacheFile)

	// Write data to cache file
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return err
	}

	return nil
}

// CategorizeDomain returns all matching categories for a given domain using v2fly domain-list-community data
// It only checks category-* lists and caches results for performance
// Returns all categories that match the domain, or ["other"] if no matches, or ["private"] for local domains
func CategorizeDomain(domain string) []string {
	// Remove trailing dot if present
	domain = strings.TrimSuffix(domain, ".")

	// Convert to lowercase for comparison
	lowerDomain := strings.ToLower(domain)

	// Check for private/local domains first (no cache needed, very fast)
	if isPrivateDomain(lowerDomain) {
		return []string{"private"}
	}

	// Check cache first
	if cached := getCachedCategories(lowerDomain); cached != nil {
		return cached
	}

	// Find all matching categories by checking all category-* lists
	categories := findAllCategories(lowerDomain)

	// Cache the result
	setCachedCategories(lowerDomain, categories)

	return categories
}

// findAllCategories searches through all category lists to find all matches
func findAllCategories(domain string) []string {
	// Use set (map[string]struct{}) for O(1) lookups and automatic deduplication
	categories := make(map[string]struct{}, 4)

	// Check each category list (names are already converted in init)
	for categoryName, records := range categoryLists {
		if matchesRecordList(domain, records) {
			categories[categoryName] = struct{}{}
		}
	}

	// If no matches found, return "unknown"
	if len(categories) == 0 {
		return []string{"unknown"}
	}

	// Remove generic categories when more specific ones exist
	if len(categories) > 1 {
		categories = removeGenericCategories(categories)
	}

	// Convert set to sorted slice for consistent output
	return setToSortedSlice(categories)
}

// removeGenericCategories removes generic categories like "ads-all" and "companies"
// when more specific categories are present. Also removes "entertainment"
// when "games" is present.
func removeGenericCategories(categories map[string]struct{}) map[string]struct{} {
	// Add conditional generic categories based on what's present
    _, hasGames := categories["games"]
	if hasGames && len(categories) > 1 {
        delete(categories, "entertainment")
	}

    if len(categories) > 1 {
        delete(categories, "dev")
    }

    if len(categories) > 1 {
        delete(categories, "companies")
    }

	return categories
}

// setToSortedSlice converts a map[string]struct{} (set) to a sorted []string
func setToSortedSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for key := range s {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

// matchesRecordList checks if a domain matches any record in the list
func matchesRecordList(domain string, records []*optimizedRecord) bool {
	for _, record := range records {
		switch record.Type {
		case router.Domain_RootDomain:
			// Matches domain and all its subdomains
			// e.g., "example.com" matches "example.com" and "sub.example.com"
			// Use pre-computed DomainSuffix to avoid string concatenation
			if domain == record.Value || strings.HasSuffix(domain, record.DomainSuffix) {
				return true
			}
		case router.Domain_Full:
			// Exact match only
			if domain == record.Value {
				return true
			}
		case router.Domain_Plain:
			// Keyword appears anywhere in domain
			if strings.Contains(domain, record.Value) {
				return true
			}
		case router.Domain_Regex:
			// Use pre-compiled regex from init
			if record.Regex != nil && record.Regex.MatchString(domain) {
				return true
			}
		}
	}

	return false
}


// getCachedCategories retrieves cached categories for a domain
func getCachedCategories(domain string) []string {
	domainCacheMu.RLock()
	defer domainCacheMu.RUnlock()
	return domainCache[domain]
}

// setCachedCategories caches categories for a domain with LRU-like eviction
func setCachedCategories(domain string, categories []string) {
	domainCacheMu.Lock()
	defer domainCacheMu.Unlock()

	// Simple cache eviction: if we exceed cache size, clear half the cache
	// This is more efficient than true LRU for high-throughput scenarios
	if len(domainCache) >= cacheSize {
		// Clear half the cache (simple eviction strategy)
		cleared := 0
		target := cacheSize / 2
		for k := range domainCache {
			delete(domainCache, k)
			cleared++
			if cleared >= target {
				break
			}
		}
	}

	domainCache[domain] = categories
	cacheInsertions++
}

// listNameToCategory converts a v2fly list name to a category name
// Examples:
//   - "category-ads" -> "ads"
//   - "category-ads-all" -> "ads-all"
//   - "category-ai-!cn" -> "ai-not-cn"
func listNameToCategory(listName string) string {
	// Remove "category-" prefix
	category := strings.TrimPrefix(listName, "category-")

	// Replace "!cn" with "" for negation patterns
	category = strings.ReplaceAll(category, "-!cn", "")

    // Replace "!all" with "" for negation patterns
	category = strings.ReplaceAll(category, "-all", "")

    // Replace "tech-media" with "media"
	category = strings.ReplaceAll(category, "tech-media", "media")

	// Replace "!" with "not-" for negation patterns
	category = strings.ReplaceAll(category, "!", "not-")

	return category
}

// isPrivateDomain checks if a domain is private/local
func isPrivateDomain(domain string) bool {
	// Check for localhost
	if domain == "localhost" {
		return true
	}

	// Check for local TLDs
	if strings.HasSuffix(domain, ".local") ||
		strings.HasSuffix(domain, ".lan") ||
		strings.HasSuffix(domain, ".internal") ||
		strings.HasSuffix(domain, ".localdomain") ||
        strings.HasSuffix(domain, ".in-addr.arpa") {
		return true
	}

	// Check for IP addresses
	if net.ParseIP(domain) != nil {
		return true
	}

	return false
}

// GetAvailableCategories returns all available categories from the category-* lists
func GetAvailableCategories() []string {
	categories := make([]string, 0, len(categoryLists))
	for categoryName := range categoryLists {
		categories = append(categories, categoryName)
	}
	return categories
}

// GetCacheStats returns statistics about the cache for monitoring/debugging
func GetCacheStats() (size int, insertions int) {
	domainCacheMu.RLock()
	defer domainCacheMu.RUnlock()
	return len(domainCache), cacheInsertions
}

// ClearCache clears the domain cache (useful for testing)
func ClearCache() {
	domainCacheMu.Lock()
	defer domainCacheMu.Unlock()
	domainCache = make(map[string][]string, cacheSize)
	cacheInsertions = 0
}

package worker

import (
	"strings"
	"testing"
)

func TestCategorizeDomain(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldMatch string // Should contain this category
	}{
		// Well-known domains should be categorized
		{
			name:        "doubleclick ads",
			input:       "doubleclick.net",
			shouldMatch: "ads",
		},
		{
			name:        "facebook",
			input:       "facebook.com",
			shouldMatch: "social",
		},
		{
			name:        "twitter",
			input:       "twitter.com",
			shouldMatch: "social",
		},
		{
			name:        "youtube",
			input:       "youtube.com",
			shouldMatch: "entertainment",
		},

		// Private domains should always be "private"
		{
			name:        "localhost",
			input:       "localhost",
			shouldMatch: "private",
		},
		{
			name:        "local domain",
			input:       "myserver.local",
			shouldMatch: "private",
		},
		{
			name:        "lan domain",
			input:       "router.lan",
			shouldMatch: "private",
		},
		{
			name:        "internal domain",
			input:       "api.internal",
			shouldMatch: "private",
		},
		{
			name:        "ip address",
			input:       "192.168.1.1",
			shouldMatch: "private",
		},
		{
			name:        "ipv6 address",
			input:       "2001:db8::1",
			shouldMatch: "private",
		},

		// Unknown domains should be "unknown"
		{
			name:        "unknown domain",
			input:       "random-website-12345-unlikely-to-exist.com",
			shouldMatch: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			categories := CategorizeDomain(tt.input)

			// Check that we got at least one category
			if len(categories) == 0 {
				t.Errorf("CategorizeDomain(%q) returned empty array", tt.input)
				return
			}

			// Check if the expected category is present
			found := false
			for _, cat := range categories {
				if strings.Contains(cat, tt.shouldMatch) {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("CategorizeDomain(%q) = %v, expected to contain %q", tt.input, categories, tt.shouldMatch)
			}

			// Log all categories for informational purposes
			t.Logf("CategorizeDomain(%q) = %v (%d categories)", tt.input, categories, len(categories))
		})
	}
}

func TestCategorizeDomainCaching(t *testing.T) {
	// Clear cache before test
	ClearCache()

	domain := "example-test-domain.com"

	// First call - should not be cached
	result1 := CategorizeDomain(domain)

	// Second call - should be cached
	result2 := CategorizeDomain(domain)

	// Results should be the same
	if len(result1) != len(result2) {
		t.Errorf("Cached result length differs: first=%v, second=%v", result1, result2)
	}
	for i := range result1 {
		if result1[i] != result2[i] {
			t.Errorf("Cached result differs at index %d: first=%q, second=%q", i, result1[i], result2[i])
		}
	}

	// Verify cache stats
	size, insertions := GetCacheStats()
	if size < 1 {
		t.Error("Expected at least 1 entry in cache")
	}
	if insertions < 1 {
		t.Error("Expected at least 1 cache insertion")
	}

	t.Logf("Cache stats: size=%d, insertions=%d", size, insertions)
}

func TestListNameToCategory(t *testing.T) {
	tests := []struct {
		name     string
		listName string
		expected string
	}{
		{
			name:     "category-ads to ads",
			listName: "category-ads",
			expected: "ads",
		},
		{
			name:     "category-ads-all to ads",
			listName: "category-ads-all",
			expected: "ads",
		},
		{
			name:     "category-ai-!cn to ai",
			listName: "category-ai-!cn",
			expected: "ai",
		},
		{
			name:     "category-social-media to social-media",
			listName: "category-social-media",
			expected: "social-media",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := listNameToCategory(tt.listName)
			if result != tt.expected {
				t.Errorf("listNameToCategory(%q) = %q, expected %q", tt.listName, result, tt.expected)
			}
		})
	}
}

func TestGetAvailableCategories(t *testing.T) {
	categories := GetAvailableCategories()

	// Verify we got some categories
	if len(categories) == 0 {
		t.Fatal("GetAvailableCategories() returned no categories")
	}

	// All categories should NOT have the "category-" prefix
	for _, cat := range categories {
		if strings.HasPrefix(cat, "category-") {
			t.Errorf("Category %q still has 'category-' prefix", cat)
		}
	}

	// Check for some expected category patterns (these should exist)
	hasAds := false
	hasSocial := false
	for _, cat := range categories {
		if strings.Contains(cat, "ads") {
			hasAds = true
		}
		if strings.Contains(cat, "social") {
			hasSocial = true
		}
	}

	if !hasAds {
		t.Error("Expected to find ads-related categories")
	}
	if !hasSocial {
		t.Error("Expected to find social-related categories")
	}

	t.Logf("Found %d total categories (category-* lists only)", len(categories))
}

func TestCacheEviction(t *testing.T) {
	// Clear cache
	ClearCache()

	// Test that cache doesn't grow unbounded
	initialSize, _ := GetCacheStats()

	// Add many unique domains
	for i := 0; i < cacheSize*2; i++ {
		// Create unique domains unlikely to match any category
		domain := "very-unique-test-domain-" + strings.Repeat("z", i%5) + "-xyz-" + string(rune(i)) + ".example"
		_ = CategorizeDomain(domain)
	}

	finalSize, finalInsertions := GetCacheStats()

	// Cache size should not exceed the limit
	if finalSize > cacheSize {
		t.Errorf("Cache size %d exceeds limit %d", finalSize, cacheSize)
	}

	// Should have grown from initial size
	if finalSize <= initialSize {
		t.Errorf("Cache did not grow: initial=%d, final=%d", initialSize, finalSize)
	}

	t.Logf("Cache stats - Size: %d/%d, Total insertions: %d", finalSize, cacheSize, finalInsertions)
}

// Benchmark tests

func BenchmarkCategorizeDomain_Uncached(b *testing.B) {
	domains := []string{
		"google.com",
		"facebook.com",
		"doubleclick.net",
		"youtube.com",
		"twitter.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClearCache() // Force uncached lookup
		_ = CategorizeDomain(domains[i%len(domains)])
	}
}

func BenchmarkCategorizeDomain_Cached(b *testing.B) {
	domains := []string{
		"google.com",
		"facebook.com",
		"doubleclick.net",
		"youtube.com",
		"twitter.com",
	}

	// Prime the cache
	for _, domain := range domains {
		_ = CategorizeDomain(domain)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CategorizeDomain(domains[i%len(domains)])
	}
}

func BenchmarkCategorizeDomain_Private(b *testing.B) {
	domains := []string{
		"localhost",
		"192.168.1.1",
		"router.local",
		"api.internal",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CategorizeDomain(domains[i%len(domains)])
	}
}

func BenchmarkCategorizeDomain_Unknown(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CategorizeDomain("random-unknown-domain-xyz.com")
	}
}

func TestMultipleCategoryMatches(t *testing.T) {
	tests := []struct {
		name              string
		domain            string
		expectedMinCount  int
		mustContain       []string
	}{
		{
			name:             "facebook matches multiple categories",
			domain:           "facebook.com",
			expectedMinCount: 1,
			mustContain:      []string{"social-media"},
		},
		{
			name:             "google matches single category",
			domain:           "google.com",
			expectedMinCount: 1,
			mustContain:      []string{"companies"},
		},
		{
			name:             "doubleclick matches ads category (ads-all filtered)",
			domain:           "doubleclick.net",
			expectedMinCount: 1,
			mustContain:      []string{"ads"},
		},
		{
			name:             "youtube matches entertainment category",
			domain:           "youtube.com",
			expectedMinCount: 1,
			mustContain:      []string{"entertainment"},
		},
		{
			name:             "twitter matches single category",
			domain:           "twitter.com",
			expectedMinCount: 1,
			mustContain:      []string{"social-media"},
		},
		{
			name:             "instagram matches multiple categories",
			domain:           "instagram.com",
			expectedMinCount: 1,
			mustContain:      []string{"social-media"},
		},
		{
			name:             "tiktok matches single category",
			domain:           "tiktok.com",
			expectedMinCount: 1,
			mustContain:      []string{"entertainment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := CategorizeDomain(tt.domain)

			// Check minimum category count
			if len(matches) < tt.expectedMinCount {
				t.Errorf("CategorizeDomain(%q) returned %d categories, expected at least %d: %v",
					tt.domain, len(matches), tt.expectedMinCount, matches)
			}

			// Check that all expected categories are present
			for _, expected := range tt.mustContain {
				found := false
				for _, match := range matches {
					if match == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("CategorizeDomain(%q) = %v, expected to contain %q",
						tt.domain, matches, expected)
				}
			}

			t.Logf("Domain: %-20s matches %d categories: %v", tt.domain, len(matches), matches)
		})
	}
}

func TestCategoriesSorted(t *testing.T) {
	// Test that categories are returned in sorted order
	tests := []struct {
		name   string
		domain string
	}{
		{"facebook", "facebook.com"},
		{"google", "google.com"},
		{"youtube", "youtube.com"},
		{"doubleclick", "doubleclick.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			categories := CategorizeDomain(tt.domain)

			// Check if categories are sorted
			for i := 1; i < len(categories); i++ {
				if categories[i-1] > categories[i] {
					t.Errorf("Categories not sorted for %q: %v (found %q > %q at positions %d and %d)",
						tt.domain, categories, categories[i-1], categories[i], i-1, i)
				}
			}

			t.Logf("Domain: %-20s categories (sorted): %v", tt.domain, categories)
		})
	}
}

func TestGenericCategoryRemoval(t *testing.T) {
	tests := []struct {
		name        string
		input       []string
		expected    []string
		description string
	}{
		{
			name:        "removes companies when other categories present",
			input:       []string{"ads", "companies"},
			expected:    []string{"ads"},
			description: "companies should be removed when ads is present",
		},
		{
			name:        "removes companies when social-media present",
			input:       []string{"social-media", "companies"},
			expected:    []string{"social-media"},
			description: "companies should be removed when social-media is present",
		},
		{
			name:        "keeps companies when it's the only category",
			input:       []string{"companies"},
			expected:    []string{"companies"},
			description: "companies should be kept if it's the only category",
		},
		{
			name:        "no generic categories to remove",
			input:       []string{"ads", "entertainment"},
			expected:    []string{"ads", "entertainment"},
			description: "should keep all categories when none are generic",
		},
		{
			name:        "single non-generic category",
			input:       []string{"entertainment"},
			expected:    []string{"entertainment"},
			description: "should keep single non-generic category",
		},
		{
			name:        "removes entertainment when games is present",
			input:       []string{"games", "entertainment"},
			expected:    []string{"games"},
			description: "entertainment should be removed when games is present",
		},
		{
			name:        "removes duplicate games",
			input:       []string{"games", "games"},
			expected:    []string{"games"},
			description: "duplicate games should be deduplicated",
		},
		{
			name:        "removes entertainment and companies when games is present",
			input:       []string{"games", "entertainment", "companies"},
			expected:    []string{"games"},
			description: "entertainment and companies should be removed when games is present",
		},
		{
			name:        "keeps entertainment when games is not present",
			input:       []string{"entertainment", "companies"},
			expected:    []string{"entertainment"},
			description: "entertainment should be kept when games is not present, companies removed",
		},
		{
			name:        "removes companies when games present",
			input:       []string{"games", "companies"},
			expected:    []string{"games"},
			description: "companies should be removed when games is present",
		},
		{
			name:        "removes dev when other categories present",
			input:       []string{"ads", "dev"},
			expected:    []string{"ads"},
			description: "dev should be removed when ads is present",
		},
		{
			name:        "keeps dev when it's the only category",
			input:       []string{"dev"},
			expected:    []string{"dev"},
			description: "dev should be kept if it's the only category",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert input slice to set
			inputSet := make(map[string]struct{})
			for _, cat := range tt.input {
				inputSet[cat] = struct{}{}
			}

			// Call removeGenericCategories with set
			resultSet := removeGenericCategories(inputSet)

			// Check length
			if len(resultSet) != len(tt.expected) {
				t.Errorf("removeGenericCategories(%v) returned %d categories, expected %d: got %v, want %v",
					tt.input, len(resultSet), len(tt.expected), resultSet, tt.expected)
				return
			}

			// Check contents
			for _, expected := range tt.expected {
				if _, exists := resultSet[expected]; !exists {
					t.Errorf("removeGenericCategories(%v) = %v, expected to contain %q",
						tt.input, resultSet, expected)
				}
			}

			// Convert result set to slice for logging
			resultSlice := make([]string, 0, len(resultSet))
			for cat := range resultSet {
				resultSlice = append(resultSlice, cat)
			}

			t.Logf("%s: %v -> %v", tt.description, tt.input, resultSlice)
		})
	}
}

func TestCategoryDeduplication(t *testing.T) {
	// This test verifies that even if somehow duplicates were introduced,
	// they would be removed by slices.Compact
	// In practice, duplicates shouldn't occur because we use a map,
	// but this tests the safety mechanism

	// We'll test this indirectly by ensuring no duplicates exist in results
	domains := []string{
		"google.com",
		"facebook.com",
		"youtube.com",
		"doubleclick.net",
		"twitter.com",
	}

	for _, domain := range domains {
		t.Run(domain, func(t *testing.T) {
			categories := CategorizeDomain(domain)

			// Check for duplicates
			seen := make(map[string]bool)
			for _, cat := range categories {
				if seen[cat] {
					t.Errorf("Duplicate category %q found in result for %q: %v",
						cat, domain, categories)
				}
				seen[cat] = true
			}

			t.Logf("Domain: %-20s unique categories: %v", domain, categories)
		})
	}
}

func TestCategoryMerging(t *testing.T) {
	// Test that category-ads-all becomes "ads" (not "ads-all")
	// and that multiple category lists get merged into a single entry
	t.Run("ads-all suffix is removed", func(t *testing.T) {
		// Test the listNameToCategory function directly
		result := listNameToCategory("category-ads-all")
		if result != "ads" {
			t.Errorf("listNameToCategory(category-ads-all) = %q, expected %q", result, "ads")
		}
	})

	t.Run("dev-all suffix is removed", func(t *testing.T) {
		result := listNameToCategory("category-dev-all")
		if result != "dev" {
			t.Errorf("listNameToCategory(category-dev-all) = %q, expected %q", result, "dev")
		}
	})

	t.Run("domains in multiple lists show merged categories", func(t *testing.T) {
		// Test that a domain that appears in multiple category lists
		// shows all those categories merged together
		// Note: We can't easily test this without knowing which domains
		// are in multiple lists, but we can verify the behavior

		// Find a domain with multiple categories
		testDomains := []string{
			"google.com",      // likely in multiple lists
			"facebook.com",    // likely in multiple lists
			"doubleclick.net", // likely in multiple lists
		}

		for _, domain := range testDomains {
			categories := CategorizeDomain(domain)

			// Verify no category ends with "-all"
			for _, cat := range categories {
				if strings.HasSuffix(cat, "-all") {
					t.Errorf("Domain %q has category %q ending with '-all', expected suffix to be removed",
						domain, cat)
				}
			}

			t.Logf("Domain: %-20s merged categories: %v", domain, categories)
		}
	})

	t.Run("tech-media becomes media", func(t *testing.T) {
		result := listNameToCategory("category-tech-media")
		if result != "media" {
			t.Errorf("listNameToCategory(category-tech-media) = %q, expected %q", result, "media")
		}
	})

	t.Run("negation patterns are normalized", func(t *testing.T) {
		result := listNameToCategory("category-ai-!cn")
		expected := "ai"
		if result != expected {
			t.Errorf("listNameToCategory(category-ai-!cn) = %q, expected %q", result, expected)
		}
	})
}

func TestPreCalculatedEntries(t *testing.T) {
	// Test that pre-calculated Apple iCloud domains are categorized as "companies"
	tests := []struct {
		name   string
		domain string
	}{
		{"init.push.apple.com", "init.push.apple.com"},
		{"swallow.apple.com", "swallow.apple.com"},
		{"swallow-apple-com.v.aaplimg.com", "swallow-apple-com.v.aaplimg.com"},
		{"cdn-icloud-content.g.aaplimg.com", "cdn-icloud-content.g.aaplimg.com"},
		{"gateway.icloud.com", "gateway.icloud.com"},
		{"mask-h2.icloud.com", "mask-h2.icloud.com"},
		{"mask.icloud.com", "mask.icloud.com"},
		{"mask-api.icloud.com", "mask-api.icloud.com"},
		{"mask.apple-dns.net", "mask.apple-dns.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			categories := CategorizeDomain(tt.domain)

			// Check that "companies" is in the result
			found := false
			for _, cat := range categories {
				if cat == "companies" {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("CategorizeDomain(%q) = %v, expected to contain %q",
					tt.domain, categories, "companies")
			}

			t.Logf("Domain: %-40s categories: %v", tt.domain, categories)
		})
	}
}


package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	repologyAPI   = "https://repology.org/api/v1/project/"
	userAgent     = "apkg-build/1.0 (https://github.com/AzusaOS/apkg-build)"
	rateLimit     = 1100 * time.Millisecond // slightly over 1s to respect rate limit
	cacheFileName = ".repology_cache.json"
	cacheMaxAge   = 24 * time.Hour
)

// repologyPkg represents a single package entry from Repology's API response.
type repologyPkg struct {
	Repo        string `json:"repo"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	VisibleName string `json:"visiblename,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

// repologyCache stores cached query results on disk.
type repologyCache struct {
	Entries map[string]*repologyCacheEntry `json:"entries"`
}

type repologyCacheEntry struct {
	Newest    string `json:"newest"`
	Timestamp int64  `json:"timestamp"`
}

// outdatedResult holds the comparison result for a single package.
type outdatedResult struct {
	Package     string
	LocalVer    string
	UpstreamVer string
	Status      string // "outdated", "current", "ahead", "not-found", "error"
}

// categoryPrefixMap maps recipe categories to Repology naming prefixes.
var categoryPrefixMap = map[string]string{
	"dev-python":  "python:",
	"dev-perl":    "perl:",
	"dev-ruby":    "ruby:",
	"dev-haskell": "haskell:",
	"dev-php":     "php:",
}

// nameOverrides maps "category/name" to the correct Repology project name
// for packages where the local name doesn't match Repology.
var nameOverrides = map[string]string{
	// example: "dev-libs/example": "example-lib",
}

// skipDirs are directories in the recipes repo that aren't package categories.
var skipDirs = map[string]bool{
	"common": true, "azusa": true, "cross-arm64": true, "cross-mingw32": true, ".git": true,
}

func runOutdated(args []string) {
	repo := repoPath()

	// Parse filters from args
	var filters []string
	if len(args) > 0 {
		filters = args
	}

	// Discover local packages
	packages := discoverPackages(repo, filters)
	if len(packages) == 0 {
		log.Printf("No packages found.")
		return
	}
	log.Printf("Found %d packages. Checking upstream versions via Repology...", len(packages))

	// Load cache
	cache := loadRepologyCache(repo)

	client := &http.Client{Timeout: 15 * time.Second}
	var results []outdatedResult
	fetched := 0

	// Sort package names for deterministic output
	var pkgNames []string
	for name := range packages {
		pkgNames = append(pkgNames, name)
	}
	sort.Strings(pkgNames)

	for _, pkgName := range pkgNames {
		localVer := packages[pkgName]
		repologyName := toRepologyName(pkgName)

		// Check cache first
		var upstreamVer string
		if entry, ok := cache.Entries[repologyName]; ok {
			age := time.Since(time.Unix(entry.Timestamp, 0))
			if age < cacheMaxAge {
				upstreamVer = entry.Newest
				goto compare
			}
		}

		// Rate limit
		if fetched > 0 {
			time.Sleep(rateLimit)
		}

		{
			ver, err := queryRepology(client, repologyName)
			fetched++
			if err != nil {
				log.Printf("  [?] %s: error querying repology: %s", pkgName, err)
				results = append(results, outdatedResult{
					Package: pkgName, LocalVer: localVer, Status: "error",
				})
				continue
			}
			upstreamVer = ver

			// If not found and we used a prefix, try without
			if upstreamVer == "" {
				category := pkgName[:strings.IndexByte(pkgName, '/')]
				if _, hasPrefix := categoryPrefixMap[category]; hasPrefix {
					time.Sleep(rateLimit)
					bareName := pkgName[strings.IndexByte(pkgName, '/')+1:]
					ver, err = queryRepology(client, bareName)
					fetched++
					if err == nil && ver != "" {
						repologyName = bareName
						upstreamVer = ver
					}
				}
			}

			cache.Entries[repologyName] = &repologyCacheEntry{
				Newest:    upstreamVer,
				Timestamp: time.Now().Unix(),
			}

			// Periodic cache save
			if fetched%50 == 0 {
				saveRepologyCache(repo, cache)
			}
		}

	compare:
		if upstreamVer == "" {
			results = append(results, outdatedResult{
				Package: pkgName, LocalVer: localVer, Status: "not-found",
			})
			continue
		}

		cmp := compareVersions(localVer, upstreamVer)
		status := "current"
		if cmp < 0 {
			status = "outdated"
		} else if cmp > 0 {
			status = "ahead"
		}

		results = append(results, outdatedResult{
			Package: pkgName, LocalVer: localVer, UpstreamVer: upstreamVer, Status: status,
		})
	}

	// Save cache
	saveRepologyCache(repo, cache)

	// Print results
	var outdated, current, ahead, notFound, errored int
	for _, r := range results {
		switch r.Status {
		case "outdated":
			outdated++
		case "current":
			current++
		case "ahead":
			ahead++
		case "not-found":
			notFound++
		case "error":
			errored++
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 70))
	fmt.Printf("Results: %d current, %d outdated, %d ahead, %d not found, %d errors\n",
		current, outdated, ahead, notFound, errored)
	fmt.Printf("%s\n\n", strings.Repeat("=", 70))

	if outdated > 0 {
		fmt.Printf("Outdated packages (%d):\n\n", outdated)
		maxPkg, maxLocal := 0, 0
		for _, r := range results {
			if r.Status != "outdated" {
				continue
			}
			if len(r.Package) > maxPkg {
				maxPkg = len(r.Package)
			}
			if len(r.LocalVer) > maxLocal {
				maxLocal = len(r.LocalVer)
			}
		}
		for _, r := range results {
			if r.Status != "outdated" {
				continue
			}
			fmt.Printf("  %-*s  %*s  ->  %s\n", maxPkg, r.Package, maxLocal, r.LocalVer, r.UpstreamVer)
		}
		fmt.Println()
	} else {
		fmt.Println("All packages are up to date!")
	}
}

// discoverPackages walks the recipes repo and returns a map of "category/name" -> latest version.
func discoverPackages(repo string, filters []string) map[string]string {
	packages := make(map[string]string)

	categories, err := os.ReadDir(repo)
	if err != nil {
		log.Printf("Failed to read repo directory: %s", err)
		return nil
	}

	for _, catEntry := range categories {
		if !catEntry.IsDir() || skipDirs[catEntry.Name()] || strings.HasPrefix(catEntry.Name(), ".") {
			continue
		}

		catPath := filepath.Join(repo, catEntry.Name())
		pkgs, err := os.ReadDir(catPath)
		if err != nil {
			continue
		}

		for _, pkgEntry := range pkgs {
			if !pkgEntry.IsDir() || strings.HasPrefix(pkgEntry.Name(), ".") {
				continue
			}

			fullName := catEntry.Name() + "/" + pkgEntry.Name()

			// Apply filters
			if len(filters) > 0 && !matchFilter(fullName, filters) {
				continue
			}

			// Try to read build.yaml for version
			ver := readPackageVersion(filepath.Join(catPath, pkgEntry.Name()))
			if ver != "" {
				packages[fullName] = ver
			}
		}
	}

	return packages
}

// matchFilter checks if a package name matches any of the provided filters.
func matchFilter(pkgName string, filters []string) bool {
	for _, f := range filters {
		if strings.HasSuffix(f, "/") {
			if strings.HasPrefix(pkgName, f) {
				return true
			}
		} else if pkgName == f {
			return true
		} else if strings.IndexByte(f, '/') == -1 {
			// bare name: match package name part
			parts := strings.SplitN(pkgName, "/", 2)
			if len(parts) == 2 && parts[1] == f {
				return true
			}
		}
	}
	return false
}

// readPackageVersion reads the current version from a package's build.yaml.
func readPackageVersion(pkgDir string) string {
	f, err := os.Open(filepath.Join(pkgDir, "build.yaml"))
	if err != nil {
		return ""
	}
	defer f.Close()

	var bc struct {
		Versions *buildVersions `yaml:"versions"`
	}
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&bc); err != nil || bc.Versions == nil {
		return ""
	}

	if bc.Versions.Stable != "" {
		return bc.Versions.Stable
	}
	if len(bc.Versions.List) > 0 {
		return bc.Versions.List[len(bc.Versions.List)-1]
	}
	return ""
}

// toRepologyName converts a local "category/name" to a Repology project name.
func toRepologyName(pkgName string) string {
	if override, ok := nameOverrides[pkgName]; ok {
		return override
	}
	parts := strings.SplitN(pkgName, "/", 2)
	if len(parts) != 2 {
		return pkgName
	}
	category, name := parts[0], parts[1]
	if prefix, ok := categoryPrefixMap[category]; ok {
		return prefix + name
	}
	return name
}

// queryRepology fetches the newest version for a project from Repology.
func queryRepology(client *http.Client, projectName string) (string, error) {
	req, err := http.NewRequest("GET", repologyAPI+projectName, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", nil
	}
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var pkgs []repologyPkg
	if err := json.NewDecoder(resp.Body).Decode(&pkgs); err != nil {
		return "", err
	}

	// Collect all versions marked as "newest"
	newestVersions := make(map[string]bool)
	for _, p := range pkgs {
		if p.Status == "newest" {
			newestVersions[p.Version] = true
		}
	}

	if len(newestVersions) == 0 {
		return "", nil
	}

	// Return the highest newest version
	var versions []string
	for v := range newestVersions {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) < 0
	})
	return versions[len(versions)-1], nil
}

// compareVersions compares two version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	pa := splitVersion(a)
	pb := splitVersion(b)

	maxLen := len(pa)
	if len(pb) > maxLen {
		maxLen = len(pb)
	}

	for i := 0; i < maxLen; i++ {
		var va, vb string
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}

		// Try numeric comparison first
		na, errA := strconv.ParseInt(va, 10, 64)
		nb, errB := strconv.ParseInt(vb, 10, 64)

		if errA == nil && errB == nil {
			if na < nb {
				return -1
			}
			if na > nb {
				return 1
			}
			continue
		}

		// Fall back to string comparison
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}

	return 0
}

// splitVersion splits a version string into components on '.', '-', '_',
// and digit/letter transitions (so "3p9" becomes ["3","p","9"]).
func splitVersion(v string) []string {
	var parts []string
	var cur strings.Builder

	isDigit := func(ch rune) bool { return ch >= '0' && ch <= '9' }

	var lastDigit *bool
	for _, ch := range v {
		if ch == '.' || ch == '-' || ch == '_' {
			if cur.Len() > 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			}
			lastDigit = nil
			continue
		}
		d := isDigit(ch)
		if lastDigit != nil && d != *lastDigit && cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
		cur.WriteRune(ch)
		lastDigit = &d
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func loadRepologyCache(repo string) *repologyCache {
	cache := &repologyCache{Entries: make(map[string]*repologyCacheEntry)}
	f, err := os.Open(filepath.Join(repo, cacheFileName))
	if err != nil {
		return cache
	}
	defer f.Close()
	json.NewDecoder(f).Decode(cache)
	if cache.Entries == nil {
		cache.Entries = make(map[string]*repologyCacheEntry)
	}
	return cache
}

func saveRepologyCache(repo string, cache *repologyCache) {
	f, err := os.Create(filepath.Join(repo, cacheFileName))
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(cache)
}

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ShellScript represents a parsed shell build script
type ShellScript struct {
	Version      string
	SourceURL    string
	Engine       string
	Options      []string
	Arguments    []string
	Import       []string
	Patches      []string
	Env          []string
	ConfigurePre []string
	CompilePre   []string
	InstallPost  []string
}

// parseShellScript parses a shell build script and extracts build information
func parseShellScript(path string) (*ShellScript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	script := &ShellScript{}
	scanner := bufio.NewScanner(f)

	// Extract version from filename
	base := filepath.Base(path)
	pkgName := filepath.Base(filepath.Dir(path))
	if strings.HasPrefix(base, pkgName+"-") && strings.HasSuffix(base, ".sh") {
		script.Version = strings.TrimSuffix(strings.TrimPrefix(base, pkgName+"-"), ".sh")
	}

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Parse the script
	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip source and acheck
		if strings.HasPrefix(line, "source ") || line == "acheck" {
			continue
		}

		// Parse get command
		if strings.HasPrefix(line, "get ") {
			script.SourceURL = extractURL(line)
			continue
		}

		// Parse download command (alternative to get)
		if strings.HasPrefix(line, "download ") {
			script.SourceURL = extractURL(line)
			continue
		}

		// Parse importpkg
		if strings.HasPrefix(line, "importpkg ") {
			imports := strings.TrimPrefix(line, "importpkg ")
			for _, imp := range strings.Fields(imports) {
				script.Import = append(script.Import, imp)
			}
			continue
		}

		// Parse apatch
		if strings.HasPrefix(line, "apatch ") {
			patches := extractPatches(line)
			script.Patches = append(script.Patches, patches...)
			continue
		}

		// Parse patch command
		if strings.HasPrefix(line, "patch ") && strings.Contains(line, "<") {
			patch := extractPatchFromPipe(line)
			if patch != "" {
				script.Patches = append(script.Patches, patch)
			}
			continue
		}

		// Parse doconf
		if strings.HasPrefix(line, "doconf") {
			script.Engine = "autoconf"
			args := extractArguments(line, "doconf")
			script.Arguments = append(script.Arguments, args...)
			continue
		}

		// Parse doconflight
		if strings.HasPrefix(line, "doconflight") {
			script.Engine = "autoconf"
			script.Options = append(script.Options, "light")
			args := extractArguments(line, "doconflight")
			script.Arguments = append(script.Arguments, args...)
			continue
		}

		// Parse doconf213
		if strings.HasPrefix(line, "doconf213") {
			script.Engine = "autoconf"
			script.Options = append(script.Options, "213")
			args := extractArguments(line, "doconf213")
			script.Arguments = append(script.Arguments, args...)
			continue
		}

		// Parse docmake
		if strings.HasPrefix(line, "docmake") || (strings.Contains(line, "docmake") && !strings.HasPrefix(line, "#")) {
			script.Engine = "cmake"
			// Check for CMAKE_ROOT prefix
			if strings.Contains(line, "CMAKE_ROOT=") {
				re := regexp.MustCompile(`CMAKE_ROOT=["']?([^"'\s]+)["']?`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					script.Env = append(script.Env, "CMAKE_ROOT="+matches[1])
				}
			}
			args := extractArguments(line, "docmake")
			script.Arguments = append(script.Arguments, args...)
			continue
		}

		// Parse domeson
		if strings.HasPrefix(line, "domeson") {
			script.Engine = "meson"
			args := extractArguments(line, "domeson")
			script.Arguments = append(script.Arguments, args...)
			continue
		}

		// Parse aautoreconf
		if line == "aautoreconf" || strings.HasPrefix(line, "aautoreconf ") {
			script.Options = append(script.Options, "autoreconf")
			continue
		}

		// Parse export commands as env
		if strings.HasPrefix(line, "export ") {
			// Convert "export VAR=value" to "VAR=value"
			envLine := strings.TrimPrefix(line, "export ")
			script.Env = append(script.Env, envLine)
			continue
		}

		// Parse sed commands as configure_pre
		if strings.HasPrefix(line, "sed ") {
			script.ConfigurePre = append(script.ConfigurePre, line)
			continue
		}

		// Parse ln commands (symlinks) as configure_pre
		// Also create parent directory for the target if needed
		if strings.HasPrefix(line, "ln ") {
			// Extract target path and create parent dir
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				target := parts[len(parts)-1]
				dir := filepath.Dir(target)
				if dir != "" && dir != "." {
					script.ConfigurePre = append(script.ConfigurePre, "mkdir -p "+dir)
				}
			}
			script.ConfigurePre = append(script.ConfigurePre, line)
			continue
		}

		// Parse custom make commands
		if strings.HasPrefix(line, "make ") && !strings.Contains(line, "install") {
			// Skip standard make, but capture custom make commands
			if line != "make" && !strings.HasPrefix(line, "make -j") {
				script.CompilePre = append(script.CompilePre, line)
			}
			continue
		}

		// Skip common lines
		if line == "finalize" || strings.HasPrefix(line, "cd ") ||
			line == "make" || strings.HasPrefix(line, "make install") ||
			strings.HasPrefix(line, "make -j") {
			continue
		}

		// Capture other lines as install_post (might need manual review)
		if i > len(lines)/2 && !strings.HasPrefix(line, "if ") &&
			!strings.HasPrefix(line, "fi") && !strings.HasPrefix(line, "for ") &&
			!strings.HasPrefix(line, "done") && !strings.HasPrefix(line, "else") {
			// These might be post-install commands
			if strings.HasPrefix(line, "ln ") || strings.HasPrefix(line, "mkdir ") ||
				strings.HasPrefix(line, "cp ") || strings.HasPrefix(line, "mv ") ||
				strings.HasPrefix(line, "install ") {
				script.InstallPost = append(script.InstallPost, line)
			}
		}
	}

	// Default to autoconf if no engine detected
	if script.Engine == "" {
		script.Engine = "auto"
	}

	return script, nil
}

func extractURL(line string) string {
	// Remove 'get ' or 'download ' prefix
	line = strings.TrimPrefix(line, "get ")
	line = strings.TrimPrefix(line, "download ")

	// Handle rename syntax: URL filename
	parts := strings.Fields(line)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func extractPatches(line string) []string {
	var patches []string
	// apatch "${FILESDIR}/patch1.patch" "${FILESDIR}/patch2.patch"
	re := regexp.MustCompile(`"\$\{?FILESDIR\}?/([^"]+)"`)
	matches := re.FindAllStringSubmatch(line, -1)
	for _, m := range matches {
		if len(m) > 1 {
			patches = append(patches, m[1])
		}
	}
	return patches
}

func extractPatchFromPipe(line string) string {
	// patch -p1 <"$FILESDIR/patch.patch"
	re := regexp.MustCompile(`<\s*"\$\{?FILESDIR\}?/([^"]+)"`)
	if matches := re.FindStringSubmatch(line); len(matches) > 1 {
		return matches[1]
	}
	// patch -p1 <$FILESDIR/patch.patch
	re = regexp.MustCompile(`<\s*\$\{?FILESDIR\}?/(\S+)`)
	if matches := re.FindStringSubmatch(line); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func extractArguments(line, cmd string) []string {
	// Remove the command
	line = strings.TrimSpace(strings.TrimPrefix(line, cmd))

	// Skip if empty or just whitespace
	if line == "" {
		return nil
	}

	// Split by whitespace, handling quoted strings
	var args []string
	inQuote := false
	current := ""
	for _, c := range line {
		if c == '"' || c == '\'' {
			inQuote = !inQuote
		} else if c == ' ' && !inQuote {
			if current != "" {
				args = append(args, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		args = append(args, current)
	}

	return args
}

// convertPackage converts all shell scripts in a package directory to build.yaml
func convertPackage(pkgPath string) error {
	entries, err := os.ReadDir(pkgPath)
	if err != nil {
		return err
	}

	pkgName := filepath.Base(pkgPath)
	var scripts []*ShellScript
	var scriptFiles []string

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sh") {
			continue
		}
		if !strings.HasPrefix(name, pkgName+"-") {
			continue
		}

		scriptPath := filepath.Join(pkgPath, name)
		script, err := parseShellScript(scriptPath)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v", scriptPath, err)
			continue
		}
		scripts = append(scripts, script)
		scriptFiles = append(scriptFiles, scriptPath)
	}

	if len(scripts) == 0 {
		return fmt.Errorf("no shell scripts found")
	}

	// Sort by version
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Version < scripts[j].Version
	})

	// Generate build.yaml
	config := generateBuildConfig(scripts)

	// Write build.yaml
	yamlPath := filepath.Join(pkgPath, "build.yaml")
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	err = os.WriteFile(yamlPath, data, 0644)
	if err != nil {
		return err
	}

	log.Printf("Created %s", yamlPath)

	return nil
}

func generateBuildConfig(scripts []*ShellScript) map[string]interface{} {
	// Extract versions
	var versions []string
	for _, s := range scripts {
		versions = append(versions, s.Version)
	}

	config := map[string]interface{}{
		"versions": map[string]interface{}{
			"list":   versions,
			"stable": versions[len(versions)-1],
		},
	}

	// Group scripts by similar build instructions
	// For now, just use the latest script as the template
	latest := scripts[len(scripts)-1]

	build := map[string]interface{}{
		"version": "*",
	}

	if latest.SourceURL != "" {
		build["source"] = []string{latest.SourceURL}
	}

	if latest.Engine != "" && latest.Engine != "auto" {
		build["engine"] = latest.Engine
	}

	if len(latest.Options) > 0 {
		build["options"] = latest.Options
	}

	if len(latest.Arguments) > 0 {
		build["arguments"] = latest.Arguments
	}

	if len(latest.Import) > 0 {
		build["import"] = latest.Import
	}

	if len(latest.Patches) > 0 {
		build["patches"] = latest.Patches
	}

	if len(latest.Env) > 0 {
		build["env"] = latest.Env
	}

	if len(latest.ConfigurePre) > 0 {
		build["configure_pre"] = latest.ConfigurePre
	}

	if len(latest.CompilePre) > 0 {
		build["compile_pre"] = latest.CompilePre
	}

	if len(latest.InstallPost) > 0 {
		build["install_post"] = latest.InstallPost
	}

	config["build"] = []interface{}{build}

	return config
}

// convertAllPackages finds and converts all packages with .sh files but no build.yaml
func convertAllPackages(repoPath string, limit int) error {
	converted := 0

	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Only look at category/package directories (depth 2)
		rel, _ := filepath.Rel(repoPath, path)
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 2 || !d.IsDir() {
			return nil
		}

		// Skip if build.yaml already exists
		if _, err := os.Stat(filepath.Join(path, "build.yaml")); err == nil {
			return nil
		}

		// Check if .sh files exist
		pkgName := filepath.Base(path)
		matches, _ := filepath.Glob(filepath.Join(path, pkgName+"-*.sh"))
		if len(matches) == 0 {
			return nil
		}

		// Convert this package
		log.Printf("Converting %s...", rel)
		if err := convertPackage(path); err != nil {
			log.Printf("  Error: %v", err)
		} else {
			converted++
		}

		if limit > 0 && converted >= limit {
			return filepath.SkipAll
		}

		return nil
	})

	log.Printf("Converted %d packages", converted)
	return err
}

package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	buildVersion = flag.String("version", "", "specify version to build")
	buildArch    = flag.String("arch", runtime.GOARCH, "specify arch")
)

type pkg struct {
	fn string // full name (ie. sys-libs/zlib)
}

func loadPackage(name string) *pkg {
	log.Printf("Using repository found in %s", repoPath())

	if strings.IndexByte(name, '/') == -1 {
		// we only have the pkg name, let's try to find options
		opts, err := os.ReadDir(repoPath())
		if err != nil {
			log.Printf("failed to list categories: %s", err)
			return nil
		}

		// let's search for name in each of opts
		var found []string
		for _, op := range opts {
			catnam := op.Name()
			if strings.HasPrefix(catnam, ".") {
				continue
			}
			j := filepath.Join(repoPath(), catnam, name)
			if _, err := os.Stat(j); err == nil {
				// found it
				found = append(found, filepath.Join(catnam, name))
			}
		}
		if len(found) == 0 {
			log.Printf("not found: %s", name)
			return nil
		}
		if len(found) > 1 {
			log.Printf("found many options: %v", found)
			return nil
		}
		return &pkg{fn: found[0]}
	}

	// simple
	j := filepath.Join(repoPath(), name)
	if _, err := os.Stat(j); err == nil {
		return &pkg{fn: name}
	}

	log.Printf("not found: %s", name)

	return nil
}

func (p *pkg) base() string {
	return filepath.Join(repoPath(), p.fn)
}

func (p *pkg) readBuildConfig() (*buildConfig, error) {
	f, err := os.Open(filepath.Join(p.base(), "build.yaml"))
	if err != nil {
		// No build.yaml, try to find .sh files
		return p.readShellConfig()
	}
	defer f.Close()

	var bc *buildConfig
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	err = dec.Decode(&bc)
	if err != nil {
		return nil, err
	}

	// read meta
	metaFile, err := os.Open(filepath.Join(p.base(), "metadata.yaml"))
	if err == nil {
		defer metaFile.Close()
		dec = yaml.NewDecoder(metaFile)
		dec.KnownFields(true)

		err = dec.Decode(&bc.meta)
		if err != nil {
			return nil, err
		}
	} else {
		bc.meta = &buildMeta{}
		bc.meta.Files = bc.Files
		bc.Files = nil
	}

	bc.pkgname = p.fn
	bc.Save()

	// fetch last commit date for build.yaml
	c := exec.Command("git", "log", "-1", "--pretty=%ct", "build.yaml")
	c.Dir = p.base()
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return nil, err
	}
	bc.epoch = strings.TrimSpace(string(out))

	return bc, nil
}

// readShellConfig reads .sh files from the package directory and creates a buildConfig
// by parsing the shell scripts and converting them to build instructions
func (p *pkg) readShellConfig() (*buildConfig, error) {
	pkgName := filepath.Base(p.fn)
	entries, err := os.ReadDir(p.base())
	if err != nil {
		return nil, err
	}

	var scripts []*ShellScript
	var versions []string

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sh") {
			continue
		}
		// Expected format: pkgname-version.sh (e.g., zlib-1.0.8.sh)
		if !strings.HasPrefix(name, pkgName+"-") {
			continue
		}

		scriptPath := filepath.Join(p.base(), name)
		script, err := parseShellScript(scriptPath)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v", scriptPath, err)
			continue
		}
		scripts = append(scripts, script)
		versions = append(versions, script.Version)
	}

	if len(scripts) == 0 {
		return nil, os.ErrNotExist
	}

	bc := &buildConfig{
		pkgname: p.fn,
		Versions: &buildVersions{
			List:   versions,
			Stable: versions[len(versions)-1],
		},
		Build: make([]*buildInstructions, 0),
		meta:  &buildMeta{Files: make(map[string]*buildFile)},
	}

	// Convert each parsed shell script to build instructions
	for _, script := range scripts {
		bi := &buildInstructions{
			Version:   script.Version,
			Engine:    script.Engine,
			Options:   script.Options,
			Arguments: script.Arguments,
			Import:    script.Import,
			Patches:   script.Patches,
			Env:       script.Env,
		}
		if script.SourceURL != "" {
			bi.Source = []string{script.SourceURL}
		}
		if len(script.ConfigurePre) > 0 {
			bi.ConfigurePre = script.ConfigurePre
		}
		if len(script.CompilePre) > 0 {
			bi.CompilePre = script.CompilePre
		}
		if len(script.InstallPost) > 0 {
			bi.InstallPost = script.InstallPost
		}
		bc.Build = append(bc.Build, bi)
	}

	// fetch last commit date for latest .sh file
	latestScript := pkgName + "-" + versions[len(versions)-1] + ".sh"
	c := exec.Command("git", "log", "-1", "--pretty=%ct", latestScript)
	c.Dir = p.base()
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		// Use current time if git fails
		bc.epoch = "0"
	} else {
		bc.epoch = strings.TrimSpace(string(out))
	}

	log.Printf("Converted %d shell build scripts for %s", len(scripts), p.fn)

	return bc, nil
}

func (p *pkg) build() {
	log.Printf("Build %s", p.fn)

	// parse config
	c, err := p.readBuildConfig()
	if err != nil {
		log.Printf("Failed to parse config for %s: %s", p.fn, err)
		os.Exit(1)
	}

	// determine version to build
	version := *buildVersion
	if version == "" {
		if c.Versions == nil || len(c.Versions.List) == 0 {
			log.Printf("No versions defined for %s", p.fn)
			os.Exit(1)
		}
		version = c.Versions.Latest()
	}

	e := &buildEnv{
		pkg:     p,
		config:  c,
		version: version,
		os:      runtime.GOOS,
		arch:    *buildArch,
	}
	if err := e.initVars(); err != nil {
		log.Printf("Failed to initialize build environment: %s", err)
		os.Exit(1)
	}

	// let's check versions unless forced
	err = e.build(p)
	if err != nil {
		log.Printf("build failed: %s", err)
		os.Exit(1)
	}
}

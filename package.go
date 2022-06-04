package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
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
			if strings.HasPrefix(name, ".") {
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
		return nil, err
	}
	defer f.Close()

	var bc *buildConfig
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	err = dec.Decode(&bc)
	if err != nil {
		return nil, err
	}

	bc.pkgname = p.fn
	bc.Save()

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

	e := &buildEnv{
		pkg:     p,
		config:  c,
		version: c.Versions.Latest(),
		os:      runtime.GOOS,
		arch:    runtime.GOARCH,
	}
	e.initVars()

	// let's check versions unless forced
	err = e.build(p)
	if err != nil {
		log.Printf("build failed: %s", err)
		os.Exit(1)
	}
}

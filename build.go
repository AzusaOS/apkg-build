package main

import (
	"errors"
	"log"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type buildEnv struct {
	pkg      *pkg
	config   *buildConfig
	version  string // 1.x
	category string // app-arch
	name     string // zlib
	vars     map[string]string
}

type buildVersions struct {
	List   []string `yaml:"list"`
	Stable string   `yaml:"stable"`
}

type buildInstructions struct {
	Version string   `yaml:"version"`
	Source  []string `yaml:"source"` // url of source (if multiple files, multiple urls)
	Engine  string   `yaml:"engine"` // build engine
	Options []string `yaml:"options,flow,omitempty"`
}

type buildConfig struct {
	pkgname  string
	Versions *buildVersions        `yaml:"versions"`
	Build    []*buildInstructions  `yaml:"build"`
	Files    map[string]*buildFile `yaml:"files,omitempty"`
}

type buildFile struct {
	Size   int64
	Hashes map[string]string
}

func (bv *buildVersions) Versions() []string {
	// get all versions
	return bv.List
}

func (bv *buildVersions) Latest() string {
	// return last version
	return bv.List[len(bv.List)-1]
}

func (bv *buildConfig) getInstructions(v string) *buildInstructions {
	for _, i := range bv.Build {
		if match, err := path.Match(i.Version, v); err != nil {
			log.Printf("skipping instructions for version %s: %s", i.Version, err)
		} else if match {
			return i
		}
	}
	return nil
}
func (bv *buildConfig) Save() error {
	tgt := filepath.Join(repoPath(), bv.pkgname, "build.yaml")
	f, err := os.Create(tgt + "~")
	if err != nil {
		return err
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)

	err = enc.Encode(bv)
	if err != nil {
		return err
	}
	enc.Close()
	f.Close()

	// rename
	return os.Rename(tgt+"~", tgt)
}

func (e *buildEnv) initVars() {
	e.category = path.Dir(e.pkg.fn) // category, eg. app-arch
	e.name = path.Base(e.pkg.fn)    // zlib

	e.vars = map[string]string{
		"P":        e.name + "-" + e.version,
		"PN":       e.name,                   // zlib
		"PF":       e.name + "-" + e.version, // pf = full (includes revision)
		"CATEGORY": e.category,
		"PV":       e.version,
		"PVR":      e.version, // TODO add revision (or strip from PV)
		"PKG":      e.category + "." + e.name,
	}
}

func (e *buildEnv) getVar(v string) string {
	r, ok := e.vars[v]
	if ok {
		return r
	}
	return ""
}

func (e *buildEnv) build(p *pkg) error {
	// let's just build latest version
	i := e.config.getInstructions(e.version)
	if i == nil {
		return errors.New("no instructions available")
	}
	log.Printf("building version %s of %s using %s", e.version, p.fn, i.Engine)

	err := e.download(i)
	if err != nil {
		return err
	}

	return nil
}

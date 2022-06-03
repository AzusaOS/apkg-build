package main

import (
	"errors"
	"log"
	"path"
)

type buildEnv struct {
	pkg    *pkg
	config *buildConfig
}

type buildVersions struct {
	List   []string `yaml:"list"`
	Stable string   `yaml:"stable"`
}

type buildInstructions struct {
	Version string   `yaml:"version"`
	Source  []string `yaml:"source"` // url of source
	Engine  string   `yaml:"engine"` // build engine
	Options []string `yaml:"options"`
}

type buildConfig struct {
	Versions *buildVersions       `yaml:"versions"`
	Build    []*buildInstructions `yaml:"build"`
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

func (e *buildEnv) build(p *pkg) error {
	// let's just build latest version
	v := e.config.Versions.Latest()
	i := e.config.getInstructions(v)
	if i == nil {
		return errors.New("no instructions available")
	}
	log.Printf("building version %s of %s using %s", v, p.fn, i.Engine)

	return nil
}

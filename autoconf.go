package main

import (
	"errors"
	"log"
	"path/filepath"
)

func (e *buildEnv) buildAutoconf(i *buildInstructions) error {
	// perform autoconf build
	opts := make(map[string]bool)

	// read options
	for _, opt := range i.Options {
		opts[opt] = true
	}

	// ensure we have up to date config.sub & config.guess
	flist := findFiles(e.workdir, "config.sub", "config.guess")
	for _, f := range flist {
		log.Printf("upgrading %s", f)
		cloneFile(filepath.Join(e.workdir, f), filepath.Join("/pkg/main/sys-devel.gnuconfig.core/share/gnuconfig", filepath.Base(f)))
	}

	return errors.New("TODO")
}

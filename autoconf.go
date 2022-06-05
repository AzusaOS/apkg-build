package main

import (
	"fmt"
	"log"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (e *buildEnv) buildAutoconf() error {
	// perform autoconf build
	opts := make(map[string]bool)

	// read options
	for _, opt := range e.i.Options {
		opts[opt] = true
	}

	// ensure we have up to date config.sub & config.guess
	flist := findFiles(e.workdir, "config.sub", "config.guess")
	for _, f := range flist {
		log.Printf("upgrading %s", f)
		cloneFile(filepath.Join(e.workdir, f), filepath.Join("/pkg/main/sys-devel.gnuconfig.core/share/gnuconfig", filepath.Base(f)))
	}

	cnf := e.findConfigure()
	if cnf == "" {
		return fmt.Errorf("could not find configure")
	}

	args := []string{cnf, "--prefix=" + e.getDir("core")}

	if _, light := opts["light"]; !light {
		// not in light mode
		args = append(args,
			"--sysconfdir=/etc",
			"--host="+e.chost,
			"--build="+e.chost,
			"--includedir="+e.getDir("dev")+"/include",
			"--libdir="+e.getDir("libs")+"/lib"+e.libsuffix,
			"--datarootdir="+e.getDir("core")+"/share",
			"--mandir="+e.getDir("doc")+"/man",
		)
		if _, mode213 := opts["213"]; !mode213 {
			// not in mode 213 either, add more
			args = append(args,
				"--docdir="+e.getDir("doc")+"/doc",
			)
		}
	}

	buildDir := e.temp

	err := e.runIn(buildDir, args[0], args[1:]...)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "make")
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "make", "install", "DESTDIR="+e.dist)
	if err != nil {
		return err
	}

	return nil
}

func (e *buildEnv) findConfigure() string {
	// find configure script
	// look into e.src/configure, and e.src/*/configure
	if e.src == "" {
		// can't find it if nothing
		return ""
	}

	t := filepath.Join(e.src, "configure")
	if err := unix.Access(t, unix.X_OK); err == nil {
		return t
	}

	// TODO

	return ""
}

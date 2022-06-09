package main

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strconv"

	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) buildAutoconf() error {
	var err error
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

	err = e.runManyIn(e.src, e.i.ConfigurePre)
	if err != nil {
		return err
	}

	if _, autoreconf := opts["autoreconf"]; autoreconf {
		log.Printf("Running autoreconf tools...")
		libtoolize := []string{"libtoolize", "--force", "--install"}
		reconf := []string{"autoreconf", "-fi", "-I", "/pkg/main/azusa.symlinks.core/share/aclocal/"}

		if _, err := e.backend.Stat(filepath.Join(e.src, "m4")); err == nil {
			reconf = append(reconf, "-I", filepath.Join(e.src, "m4"))
		}

		err = e.runIn(e.src, libtoolize...)
		if err != nil {
			return err
		}
		err = e.runIn(e.src, reconf...)
		if err != nil {
			return err
		}
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

	for _, arg := range e.i.Arguments {
		arg, err = shell.Expand(arg, e.getVar)
		if err != nil {
			return err
		}
		args = append(args, arg)
	}

	buildDir := e.temp

	if _, inPlace := opts["build_in_tree"]; inPlace {
		buildDir = filepath.Dir(cnf)
	}

	err = e.runIn(buildDir, args...)
	if err != nil {
		return err
	}

	err = e.runManyIn(filepath.Dir(cnf), e.i.ConfigurePost)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "make", "-j"+strconv.Itoa(runtime.NumCPU()))
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePost)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "make", "install", "DESTDIR="+e.dist, "LDCONFIG=/bin/true")
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPost)
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
	if st, err := e.backend.Stat(t); err == nil && st.Mode()&1 == 1 {
		return t
	}

	// TODO

	return ""
}

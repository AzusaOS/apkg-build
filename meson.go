package main

import (
	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) buildMeson() error {
	// build custom rules (gentoo inspired)

	// allow override of mesonRoot via MESON_ROOT
	mesonRoot := e.src
	if v := e.getVar("MESON_ROOT"); v != "" {
		mesonRoot = v
	}

	mesonOpts := []string{
		"meson",
		mesonRoot,
		"--prefix=" + e.getDir("core"),
		"--libdir=" + e.getDir("libs") + "/lib" + e.libsuffix,
		"--includedir=" + e.getDir("dev") + "/include",
		"--mandir=" + e.getDir("doc") + "/man",
		"-Dbuildtype=release",
	}

	// Process custom arguments from build.yaml
	for _, arg := range e.i.Arguments {
		arg, err := shell.Expand(arg, e.getVar)
		if err != nil {
			return err
		}
		mesonOpts = append(mesonOpts, arg)
	}

	buildDir := e.temp

	err := e.runManyIn(buildDir, e.i.ConfigurePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, mesonOpts...)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.ConfigurePost)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "ninja")
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePost)
	if err != nil {
		return err
	}

	// let meson know of our DESTDIR
	e.vars["DESTDIR"] = e.dist

	err = e.runManyIn(buildDir, e.i.InstallPre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "ninja", "install")
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPost)
	if err != nil {
		return err
	}

	return nil
}

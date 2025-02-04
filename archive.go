package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (e *buildEnv) archive() error {
	infofile := filepath.Join(repoPath(), e.config.pkgname, "azusa.yaml")
	if _, err := os.Stat(infofile); err == nil {
		// there's a azusa.yaml file
		tgt := filepath.Join(e.dist, e.getDir("core"))
		e.backend.MkdirAll(tgt, 0755)
		err := e.backend.PutFile(infofile, filepath.Join(tgt, "azusa.yaml"))
		if err != nil {
			return fmt.Errorf("failed to copy azusa.yaml: %w", err)
		}
	}

	// TODO: scan for suid files, show warning if any

	libdir := filepath.Join(e.dist, e.getDir("libs"))
	buf := &bytes.Buffer{}

	for _, sub := range []string{"lib64", "lib32", "lib"} {
		st, err := e.backend.Lstat(filepath.Join(libdir, sub))
		if err != nil {
			// does not exist?
			continue
		}
		if st.Mode().Type() == fs.ModeSymlink {
			continue
		}
		if st.Mode().IsDir() {
			// append without e.dist
			fmt.Fprintf(buf, "%s\n", filepath.Join(e.getDir("libs"), "lib"+e.libsuffix, sub))
		}
	}

	if buf.Len() > 0 {
		// run ldconfig
		err := e.backend.WriteFile(filepath.Join(e.dist, e.getDir("libs"), ".ld.so.conf"), buf.Bytes(), 0644)
		if err != nil {
			return fmt.Errorf("while creating .ld.so.conf: %w", err)
		}
		err = e.run("/pkg/main/sys-libs.glibc.core/sbin/ldconfig", "--format=new", "-r", e.dist, "-C", filepath.Join(e.getDir("libs"), ".ld.so.cache"), "-f", filepath.Join(e.getDir("libs"), ".ld.so.conf"))
		if err != nil {
			return fmt.Errorf("while running ldconfig: %w", err)
		}
	}

	// let's run squashfs
	list, err := e.backend.ReadDir(filepath.Join(e.dist, "pkg", "main"))
	if err != nil {
		return err
	}

	apkgOut := "/tmp/apkg"
	e.backend.MkdirAll(apkgOut, 0755)
	if !e.backend.IsLocal() {
		// also make dir locally if using qemu
		os.MkdirAll(apkgOut, 0755)
	}

	for _, nfo := range list {
		sub := nfo.Name()
		squash := filepath.Join(apkgOut, sub+".squashfs")

		if e.backend.IsRoot() {
			err = e.run("mksquashfs", filepath.Join(e.dist, "pkg", "main", sub), squash, "-nopad", "-noappend")
		} else {
			err = e.run("mksquashfs", filepath.Join(e.dist, "pkg", "main", sub), squash, "-all-root", "-nopad", "-noappend")
		}
		if err != nil {
			return fmt.Errorf("while running mksquashfs: %w", err)
		}
		if !e.backend.IsLocal() {
			// fetch file locally
			err = e.backend.GetFile(squash, squash)
			if err != nil {
				return fmt.Errorf("while fetching from qemu: %w", err)
			}
		}
		if e.backend.IsRoot() {
			// copy to /var/lib/apkg/unsigned
			e.run("cp", squash, filepath.Join("/var/lib/apkg/unsigned", filepath.Base(squash)))
		}
	}

	return nil
}

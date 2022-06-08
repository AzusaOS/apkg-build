package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func (e *buildEnv) archive() error {
	infofile := filepath.Join(repoPath(), e.config.pkgname, "azusa.yaml")
	if _, err := e.Stat(infofile); err == nil {
		// there's a azusa.yaml file
		tgt := filepath.Join(e.dist, e.getDir("core"))
		e.MkdirAll(tgt, 0755)
		err := cloneFile(filepath.Join(tgt, "azusa.yaml"), infofile)
		if err != nil {
			return fmt.Errorf("failed to copy azusa.yaml: %w", err)
		}
	}

	// TODO: scan for suid files, show warning if any

	libdir := filepath.Join(e.dist, e.getDir("libs"))
	buf := &bytes.Buffer{}

	for _, sub := range []string{"lib64", "lib32", "lib"} {
		st, err := e.Lstat(filepath.Join(libdir, sub))
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
		err := e.WriteFile(filepath.Join(e.dist, e.getDir("libs"), ".ld.so.conf"), buf.Bytes(), 0644)
		if err != nil {
			return fmt.Errorf("while creating .ld.so.conf: %w", err)
		}
		err = e.run("/pkg/main/sys-libs.glibc.core/sbin/ldconfig", "--format=new", "-r", e.dist, "-C", filepath.Join(e.getDir("libs"), ".ld.so.cache"), "-f", filepath.Join(e.getDir("libs"), ".ld.so.conf"))
		if err != nil {
			return fmt.Errorf("while running ldconfig: %w", err)
		}
	}

	// let's run squashfs
	list, err := e.ReadDir(filepath.Join(e.dist, "pkg", "main"))
	if err != nil {
		return err
	}

	apkgOut := "/tmp/apkg"
	e.MkdirAll(apkgOut, 0755)
	if e.qemu != nil {
		// also make dir locally if using qemu
		os.MkdirAll(apkgOut, 0755)
	}

	for _, nfo := range list {
		sub := nfo.Name()
		squash := filepath.Join(apkgOut, sub+".squashfs")

		if e.qemu != nil || os.Getuid() == 0 {
			err = e.run("mksquashfs", filepath.Join(e.dist, "pkg", "main", sub), squash, "-nopad", "-noappend")
		} else {
			err = e.run("mksquashfs", filepath.Join(e.dist, "pkg", "main", sub), squash, "-all-root", "-nopad", "-noappend")
		}
		if err != nil {
			return fmt.Errorf("while running mksquashfs: %w", err)
		}
		if e.qemu != nil {
			// fetch file locally
			err = e.qemu.fetchFile(squash, squash)
			if err != nil {
				return fmt.Errorf("while fetching from qemu: %w", err)
			}
		}
		h := hashFile(squash)
		log.Printf("hash: %+v", h)
	}

	return nil
}

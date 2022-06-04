package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

func (e *buildEnv) organize() error {
	if err := e.orgMoveLib(); err != nil {
		return err
	}
	return nil
}

func (e *buildEnv) orgMoveLib() error {
	log.Printf("Fixing libs...")
	// remove any .la file
	// see: https://wiki.gentoo.org/wiki/Project:Quality_Assurance/Handling_Libtool_Archives
	for _, p := range findFiles(e.dist, "*.la") {
		log.Printf("remove: %s", p)
		os.Remove(p)
	}

	for _, sub := range []string{"lib", "lib32", "lib64"} {
		st, err := os.Lstat(filepath.Join(e.dist, e.getDir("core"), sub))
		if err != nil {
			continue
		}
		if st.Mode().Type() == fs.ModeSymlink {
			continue
		}

		err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), sub), filepath.Join(e.getDir("libs"), sub))
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *buildEnv) moveAndLinkDir(src, dst string) error {
	// src & dst start with "/pkg/main" - need to prepend e.dist if using
	if _, err := os.Stat(filepath.Join(e.dist, dst)); err != nil {
		err = os.MkdirAll(dst, 0755)
		if err != nil {
			return err
		}
	}
	list, err := os.ReadDir(filepath.Join(e.dist, src))
	if err != nil {
		return err
	}
	for _, fi := range list {
		nam := fi.Name()
		err = os.Rename(filepath.Join(e.dist, src, nam), filepath.Join(e.dist, dst, nam))
		if err != nil {
			return err
		}
	}
	// remove dir
	err = os.Remove(filepath.Join(e.dist, src))
	if err != nil {
		return err
	}
	// symlink to dst
	return os.Symlink(dst, filepath.Join(e.dist, src))
}

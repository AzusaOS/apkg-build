package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func cloneFile(src, tgt string) error {
	// let's first try to hardlink (will fail if not on the same partition)
	err := os.Link(src, tgt)
	if err == nil {
		return nil
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	out, err := os.Create(tgt)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, f)
	if err != nil {
		return err
	}

	if st, err := f.Stat(); err == nil {
		os.Chmod(tgt, st.Mode())
	}

	return nil
}

func quickMatch(pattern, f string) bool {
	m, _ := filepath.Match(pattern, f)
	return m
}

func findFiles(dir string, fnList ...string) []string {
	// find all instances of fn in dir
	var res []string
	dir = filepath.Clean(dir)

	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		match := false
		base := filepath.Base(path)
		for _, fn := range fnList {
			if quickMatch(fn, base) {
				match = true
				break
			}
		}
		if !match {
			return nil
		}
		if d.Type()&fs.ModeType != 0 {
			// not a file
			return nil
		}
		// make path relative to dir
		if p, err := filepath.Rel(dir, path); err == nil {
			path = p
		} else {
			// try more simple
			path = strings.TrimPrefix(path, dir)
		}
		res = append(res, path)
		return nil
	})

	return res
}

func trimOsArch(v string) string {
	// we expect v to end in OS and ARCH, such as .linux.amd64 or .any.any
	// let's simply trim twice
	p := strings.LastIndexByte(v, '.')
	if p != -1 {
		v = v[:p]
	}
	p = strings.LastIndexByte(v, '.')
	if p != -1 {
		v = v[:p]
	}
	return v
}

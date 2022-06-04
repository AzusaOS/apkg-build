package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func cloneFile(tgt, src string) error {
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
	return nil
}

func quickMatch(pattern, f string) bool {
	m, _ := filepath.Match(pattern, f)
	return m
}

func findFiles(dir string, fnList ...string) []string {
	// find all instances of fn in dir
	var res []string

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
		res = append(res, path)
		return nil
	})

	return res
}

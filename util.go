package main

import (
	"io"
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

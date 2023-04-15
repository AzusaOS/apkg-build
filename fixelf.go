package main

import (
	"io/fs"
	"log"
	"strings"

	"golang.org/x/sys/unix"
)

func (e *buildEnv) fixElf() error {
	fixelf := "/pkg/main/dev-util.patchelf.core/bin/patchelf"
	// if fixelf is not available, send a warning but do not fail
	if err := unix.Access(fixelf, unix.X_OK); err != nil {
		log.Printf("WARNING: could not run fixelf in %s: %s", fixelf, err)
		return nil
	}

	log.Printf("Running fixelf...")

	return e.backend.WalkDir(e.dist, func(path string, d fs.DirEntry, err error) error {
		if !d.Type().IsRegular() {
			// not a regular file → ignore
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}

		if st.Mode().Perm()&0100 == 0 {
			// not executable
			return nil
		}

		// try to call fixelf --print-interpreter path
		val, err := e.runCaptureSilent(fixelf, "--print-interpreter", path)
		if err != nil {
			// maybe not a dynamic executable, skip it
			return nil
		}
		interp := strings.TrimSpace(string(val))
		changeto := ""

		switch interp {
		case "/lib64/ld-linux-x86-64.so.2":
			changeto = "/pkg/main/sys-libs.glibc.libs.linux.amd64/lib64/ld-linux-x86-64.so.2"
		case "/pkg/main/sys-libs.glibc.libs.linux.amd64/lib64/ld-linux-x86-64.so.2":
			// good
		case "/lib/ld-linux.so.2":
			changeto = "/pkg/main/sys-libs.glibc.libs.linux.386/lib/ld-linux.so.2"
		case "/pkg/main/sys-libs.glibc.libs.linux.386/lib/ld-linux.so.2":
			// good
		case "/lib/ld-linux-aarch64.so.1":
			changeto = "/pkg/main/sys-libs.glibc.libs.linux.arm64/lib/ld-linux-aarch64.so.1"
		case "/pkg/main/sys-libs.glibc.libs.linux.arm64/lib/ld-linux-aarch64.so.1":
			// good
		case "":
			// static file
		default:
			log.Printf("Unknown interpreter: %s", interp)
		}

		if changeto == "" {
			return nil
		}

		return e.run(fixelf, "--set-interpreter", changeto, path)
	})
}

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func (e *buildEnv) applyPatches() error {
	for _, patch := range e.i.Patches {
		// get full path
		fn := filepath.Join(repoPath(), e.config.pkgname, "files", patch)

		// check for patch file
		_, err := os.Stat(fn)
		if err != nil {
			return err
		}

		if e.qemu != nil {
			fn2 := filepath.Join("/tmp", filepath.Base(fn))
			err = e.cloneFile(fn2, fn)
			if err != nil {
				return err
			}
			fn = fn2
		}

		// apply patch
		log.Printf("Applying patch %s", patch)

		for _, plevel := range []int{1, 0, 2} {
			// attempt to apply patch
			err = e.runIn(e.src, "patch", fmt.Sprintf("-p%d", plevel), "-Nt", "-i", fn)
			if err == nil {
				break
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

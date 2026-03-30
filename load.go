package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// loadArchive waits for the just-archived squashfs packages to be loaded
// by apkg in the build VM. archive() already copies squashfs files to
// /var/lib/apkg/unsigned/ when running as root, and apkg watches that
// directory via inotify for hot-reload.
func (e *buildEnv) loadArchive() error {
	list, err := e.backend.ReadDir(filepath.Join(e.dist, "pkg", "main"))
	if err != nil {
		return err
	}

	for _, nfo := range list {
		sub := nfo.Name()
		target := filepath.Join("/pkg/main", sub)
		log.Printf("Waiting for %s to be available...", target)

		for i := 0; i < 30; i++ {
			if _, err := e.backend.Stat(target); err == nil {
				log.Printf("Package %s is ready", sub)
				break
			}
			time.Sleep(time.Second)
		}
		if _, err := e.backend.Stat(target); err != nil {
			return fmt.Errorf("timeout waiting for %s to be mounted by apkg", target)
		}
	}

	return nil
}

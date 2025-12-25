package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// buildShell executes a shell script from the recipe repository
// Shell scripts are self-contained and handle download, build, and finalize themselves
func (e *buildEnv) buildShell() error {
	// Find the script for this version
	scriptPath, ok := e.config.shellScripts[e.version]
	if !ok {
		return fmt.Errorf("no shell script found for version %s", e.version)
	}

	log.Printf("Executing shell build script: %s", scriptPath)

	// The shell scripts expect to be run from the package directory
	// and they source ../../common/init.sh which sets up the environment
	scriptDir := filepath.Dir(scriptPath)

	// Check if we're building locally or via QEMU
	if e.backend.IsLocal() {
		// Run the script directly
		cmd := exec.Command("/bin/bash", scriptPath)
		cmd.Dir = scriptDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Set ROOTDIR to point to the recipe repository root
		cmd.Env = append(os.Environ(),
			"ROOTDIR="+repoPath(),
			"ARCH="+e.arch,
		)

		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("shell build failed: %w", err)
		}
	} else {
		// For QEMU builds, we need to copy the script and common files to the remote
		// and execute there

		// First, copy the entire recipe repository's common directory
		commonDir := filepath.Join(repoPath(), "common")
		remoteCommon := "/tmp/apkg-recipes/common"
		e.backend.MkdirAll(remoteCommon, 0755)

		// Copy common files
		entries, err := os.ReadDir(commonDir)
		if err != nil {
			return fmt.Errorf("failed to read common dir: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			src := filepath.Join(commonDir, entry.Name())
			dst := filepath.Join(remoteCommon, entry.Name())
			if err := e.backend.PutFile(src, dst); err != nil {
				return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
			}
		}

		// Copy the package directory
		pkgDir := filepath.Dir(scriptPath)
		category := filepath.Base(filepath.Dir(pkgDir))
		pkgName := filepath.Base(pkgDir)
		remotePkgDir := filepath.Join("/tmp/apkg-recipes", category, pkgName)
		e.backend.MkdirAll(remotePkgDir, 0755)

		// Copy package files
		pkgEntries, err := os.ReadDir(pkgDir)
		if err != nil {
			return fmt.Errorf("failed to read package dir: %w", err)
		}
		for _, entry := range pkgEntries {
			src := filepath.Join(pkgDir, entry.Name())
			dst := filepath.Join(remotePkgDir, entry.Name())
			if entry.IsDir() {
				// Copy directory contents (e.g., files/)
				if entry.Name() == "files" {
					e.backend.MkdirAll(dst, 0755)
					subEntries, _ := os.ReadDir(src)
					for _, subEntry := range subEntries {
						if !subEntry.IsDir() {
							subSrc := filepath.Join(src, subEntry.Name())
							subDst := filepath.Join(dst, subEntry.Name())
							e.backend.PutFile(subSrc, subDst)
						}
					}
				}
			} else {
				if err := e.backend.PutFile(src, dst); err != nil {
					return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
				}
			}
		}

		// Execute the script remotely
		remoteScript := filepath.Join(remotePkgDir, filepath.Base(scriptPath))
		err = e.backend.RunEnv(remotePkgDir, []string{"/bin/bash", remoteScript},
			[]string{
				"HOME=/root",
				"PATH=/build/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"ROOTDIR=/tmp/apkg-recipes",
				"ARCH=" + e.arch,
			}, nil, nil)
		if err != nil {
			return fmt.Errorf("remote shell build failed: %w", err)
		}
	}

	return nil
}

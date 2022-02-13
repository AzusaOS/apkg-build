package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func repoPath() string {
	// in os.UserCacheDir() or /tmp if fails
	p := "/tmp"
	if c, err := os.UserCacheDir(); err == nil {
		p = c
	}
	p = filepath.Join(p, "apkg-recipes")

	return p
}

func checkRepo() {
	p := repoPath()

	if _, err := os.Stat(p); err != nil {
		// perform initial checkout
		log.Printf("Repository not found, checking out...")

		c := exec.Command("git", "clone", "https://github.com/AzusaOS/azusa-opensource-recipes.git", p)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		err := c.Run()
		if err != nil {
			log.Printf("failed to checkout: %s", err)
			os.Exit(1)
		}
	}
}

func updateRepo() error {
	p := repoPath()

	c := exec.Command("git", "pull")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = p

	return c.Run()
}

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) download(i *buildInstructions) error {
	cacheDir := "/tmp/apkg-data"

	for _, u := range i.Source {
		// TODO need to find a way to specify a different name for saved file, for example gentoo's " -> "
		u, err := shell.Expand(u, e.getVar)
		if err != nil {
			return err
		}

		fn := path.Base(u)
		tgt := filepath.Join(cacheDir, fn)
		cacheUrl := "https://pkg.azusa.jp/src/main/" + e.category + "/" + e.name + "/" + fn

		st, err := os.Stat(tgt)

		if err != nil {
			// let's download data
			os.MkdirAll(cacheDir, 0755)
			err = doDownload(tgt, cacheUrl)
			if err != nil {
				// retry
				err = doDownload(tgt, u)
			}
			if err != nil {
				return err
			}
			st, err = os.Stat(tgt)
			if err != nil {
				return err
			}
		}

		// check checksums
		log.Printf("Checking %s", fn)

		cksum := hashFile(tgt)
		if cksum == nil {
			return errors.New("failed to compute hash")
		}

		updated := false
		info, ok := e.config.Files[fn]
		if !ok {
			updated = true
			info = &buildFile{
				Size:   st.Size(),
				Hashes: make(map[string]string),
			}
			if e.config.Files == nil {
				e.config.Files = make(map[string]*buildFile)
			}
			e.config.Files[fn] = info
		} else {
			if info.Size != st.Size() {
				return fmt.Errorf("invalid file size for %s", fn)
			}
		}

		for hashName, value := range cksum {
			if goodval, ok := info.Hashes[hashName]; ok {
				if goodval != value {
					return fmt.Errorf("failed checking %s: %s hash value fail", fn, hashName)
				}
			} else {
				info.Hashes[hashName] = value
				updated = true
			}
		}

		if updated {
			e.config.Save()
		}

		// copy file to work
		workTgt := filepath.Join(e.workdir, fn)
		err = cloneFile(workTgt, tgt)
		if err != nil {
			return err
		}

		// try to extract file
		log.Printf("attempting to extract file...")

		var c *exec.Cmd
		switch {
		case quickMatch("*.zip", fn):
			c = exec.Command("unzip", "-q", fn)
		case quickMatch("*.tar.*", fn), quickMatch("*.tgz", fn), quickMatch("*.tbz2", fn):
			c = exec.Command("tar", "xf", fn)
		case quickMatch("*.gz", fn):
			c = exec.Command("gunzip", fn)
		case quickMatch("*.xz", fn):
			c = exec.Command("xz", "-d", fn)
		}

		if c != nil {
			// run c
			c.Dir = e.workdir
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			e.setCmdEnv(c)
			log.Printf("Running: %s", c)
			err = c.Run()
			if err != nil {
				log.Printf("Failed: %s", err)
			}
		}
	}

	return nil
}

func (e *buildEnv) setCmdEnv(c *exec.Cmd) {
	var env []string

	env = append(env, "HOSTNAME=localhost", "HOME="+e.base)
	for k, v := range e.vars {
		env = append(env, k+"="+v)
	}

	c.Env = env
}

func doDownload(tgt string, srcurl string) error {
	log.Printf("Attempting to download: %s", srcurl)
	// download url to tgt
	resp, err := http.Get(srcurl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP error %s", resp.Status)
	}
	// open out file
	out, err := os.Create(tgt + "~")
	defer out.Close()
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	err = out.Close()
	if err != nil {
		return err
	}
	return os.Rename(tgt+"~", tgt)
}

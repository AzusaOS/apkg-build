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
	"strings"
	"time"

	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) download() error {
	cacheDir := "/tmp/apkg-data"

	for _, u := range e.i.Source {
		// TODO need to find a way to specify a different name for saved file, for example gentoo's " -> "
		u, err := shell.Expand(u, e.getVar)
		if err != nil {
			return err
		}

		fn := path.Base(u)
		p := strings.Index(u, " -> ")
		if p != -1 {
			fn = u[p+4:]
			u = u[:p]
		}

		tgt := filepath.Join(cacheDir, fn)
		cacheUrl := "https://pkg.azusa.jp/src/main/" + e.category + "/" + e.name + "/" + fn
		needUpload := false

		st, err := os.Stat(tgt)

		if err != nil {
			// let's download data
			os.MkdirAll(cacheDir, 0755)
			err = doDownload(tgt, cacheUrl)
			if err != nil {
				needUpload = true
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
		info, ok := e.config.meta.Files[fn]
		if !ok {
			updated = true
			info = &buildFile{
				Size:   st.Size(),
				Added:  time.Now(),
				Hashes: make(map[string]string),
			}
			if e.config.meta.Files == nil {
				e.config.meta.Files = make(map[string]*buildFile)
			}
			e.config.meta.Files[fn] = info
		} else {
			if info.Size != st.Size() {
				return fmt.Errorf("invalid file size for %s", fn)
			}
			if info.Added.IsZero() {
				updated = true
				info.Added = time.Now()
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
		if needUpload {
			// upload file to the cache
			c := exec.Command("aws", "s3", "cp", tgt, "s3://azusa-pkg/src/main/"+e.category+"/"+e.name+"/"+fn)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Run()
		}

		// copy file to work
		workTgt := filepath.Join(e.workdir, fn)
		err = e.backend.PutFile(tgt, workTgt)
		if err != nil {
			return err
		}

		// try to extract file
		log.Printf("attempting to extract file...")

		var c []string
		switch {
		case quickMatch("*.zip", fn):
			c = []string{"unzip", "-q", fn}
		case quickMatch("*.tar.*", fn), quickMatch("*.tgz", fn), quickMatch("*.tbz2", fn):
			c = []string{"tar", "xf", fn}
		case quickMatch("*.gz", fn):
			c = []string{"gunzip", fn}
		case quickMatch("*.xz", fn):
			c = []string{"xz", "-d", fn}
		}

		if c != nil {
			// run c
			err = e.runIn(e.workdir, c...)
			if err != nil {
				// do not fail if it fails
				log.Printf("Failed: %s", err)
			}

			// detect dir name if we don't have one yet
			if e.src == "" {
				list, _ := e.backend.ReadDir(e.workdir)
				for _, f := range list {
					if f.IsDir() {
						// found it
						e.src = filepath.Join(e.workdir, f.Name())
						e.vars["S"] = e.src
						break
					}
				}
			}
		}
	}

	return nil
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

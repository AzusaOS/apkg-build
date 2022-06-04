package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

type buildEnv struct {
	pkg       *pkg
	i         *buildInstructions
	os        string // "linux"
	arch      string // "amd64"
	config    *buildConfig
	version   string // 1.x
	pvr       string // 1.x-r1
	pvrf      string // 1.x-r1.linux.amd64
	category  string // app-arch
	name      string // zlib
	bits      int
	chost     string // i686-pc-linux-gnu, x86_64-pc-linux-gnu, etc
	libsuffix string // "64" or ""
	vars      map[string]string

	base    string // base path for build
	workdir string // WORKDIR=$PKGBASE/work
	dist    string // D=$PKGBASE/dist
	temp    string // T=$PKGBASE/temp
	src     string // S=...
}

type buildVersions struct {
	List   []string `yaml:"list"`
	Stable string   `yaml:"stable"`
}

type buildInstructions struct {
	Version string   `yaml:"version"`
	Source  []string `yaml:"source"` // url of source (if multiple files, multiple urls)
	Engine  string   `yaml:"engine"` // build engine
	Options []string `yaml:"options,flow,omitempty"`
}

type buildConfig struct {
	pkgname  string
	Versions *buildVersions        `yaml:"versions"`
	Build    []*buildInstructions  `yaml:"build"`
	Files    map[string]*buildFile `yaml:"files,omitempty"`
}

type buildFile struct {
	Size   int64
	Hashes map[string]string
}

func (bv *buildVersions) Versions() []string {
	// get all versions
	return bv.List
}

func (bv *buildVersions) Latest() string {
	// return last version
	return bv.List[len(bv.List)-1]
}

func (bv *buildConfig) getInstructions(v string) *buildInstructions {
	for _, i := range bv.Build {
		if match, err := path.Match(i.Version, v); err != nil {
			log.Printf("skipping instructions for version %s: %s", i.Version, err)
		} else if match {
			return i
		}
	}
	return nil
}
func (bv *buildConfig) Save() error {
	tgt := filepath.Join(repoPath(), bv.pkgname, "build.yaml")
	f, err := os.Create(tgt + "~")
	if err != nil {
		return err
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)

	err = enc.Encode(bv)
	if err != nil {
		return err
	}
	enc.Close()
	f.Close()

	// rename
	return os.Rename(tgt+"~", tgt)
}

func (e *buildEnv) initVars() {
	e.category = path.Dir(e.pkg.fn) // category, eg. app-arch
	e.name = path.Base(e.pkg.fn)    // zlib

	tmpbase := "/build"
	if err := unix.Access("/build", unix.W_OK); err != nil {
		// can't use /build
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		tmpbase = filepath.Join(home, "tmp", "build")
	}

	e.base = filepath.Join(tmpbase, e.name+"-"+e.version)
	e.workdir = filepath.Join(e.base, "work")
	e.dist = filepath.Join(e.base, "dist")
	e.temp = filepath.Join(e.base, "temp")

	e.pvr = e.version // TODO revision
	e.pvrf = e.pvr + "." + e.os + "." + e.arch

	switch e.arch {
	case "386":
		e.chost = "i686-pc-linux-gnu"
		e.bits = 32
	case "amd64":
		e.chost = "x86_64-pc-linux-gnu"
		e.bits = 64
		e.libsuffix = "64"
	case "arm64":
		e.chost = "aarch64-unknown-linux-gnu"
		e.bits = 64
	default:
		log.Printf("ERROR: unsupported arch %s", e.arch)
		panic(fmt.Sprintf("ERROR: unsupported arch %s", e.arch))
	}

	// cleanup
	os.RemoveAll(e.base)
	os.MkdirAll(e.base, 0755)
	for _, sub := range []string{"work", "dist", "temp"} {
		os.Mkdir(filepath.Join(e.base, sub), 0755)
	}

	log.Printf("Using %s as build directory", e.base)

	e.vars = map[string]string{
		"P":        e.name + "-" + e.version,
		"PN":       e.name,                   // zlib
		"PF":       e.name + "-" + e.version, // pf = full (includes revision)
		"CATEGORY": e.category,
		"PV":       e.version,
		"PVR":      e.pvr, // TODO add revision (or strip from PV)
		"PVRF":     e.pvrf,
		"PKG":      e.category + "." + e.name,
		"WORKDIR":  e.workdir,
		"D":        e.dist,
		"T":        e.temp,
		"CHOST":    e.chost,
		"ARCH":     e.arch,
		"OS":       e.os,
		"BITS":     strconv.FormatInt(int64(e.bits), 10),
	}
}

func (e *buildEnv) getVar(v string) string {
	r, ok := e.vars[v]
	if ok {
		return r
	}
	return ""
}

func (e *buildEnv) build(p *pkg) error {
	// let's just build latest version
	e.i = e.config.getInstructions(e.version)
	if e.i == nil {
		return errors.New("no instructions available")
	}
	log.Printf("building version %s of %s using %s", e.version, p.fn, e.i.Engine)

	err := e.download()
	if err != nil {
		return err
	}

	// ok things are downloaded, now let's see what engine we're using
	switch e.i.Engine {
	case "autoconf":
		if err := e.buildAutoconf(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported engine: %s", e.i.Engine)
	}

	// finalize process: fixelf, organize, archive
	if err := e.fixElf(); err != nil {
		return err
	}
	if err := e.organize(); err != nil {
		return err
	}
	if err := e.archive(); err != nil {
		return err
	}

	return nil
}

func (e *buildEnv) setCmdEnv(c *exec.Cmd) {
	var env []string

	env = append(env, "HOSTNAME=localhost", "HOME="+e.base, "PATH=/usr/sbin:/usr/bin:/sbin:/bin")
	for k, v := range e.vars {
		env = append(env, k+"="+v)
	}

	c.Env = env
}

func (e *buildEnv) run(arg0 string, args ...string) error {
	log.Printf("build: running %s %s", arg0, strings.Join(args, " "))
	cmd := exec.Command(arg0, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	e.setCmdEnv(cmd)

	return cmd.Run()
}

func (e *buildEnv) runIn(dir string, arg0 string, args ...string) error {
	log.Printf("build: running %s %s", arg0, strings.Join(args, " "))
	cmd := exec.Command(arg0, args...)

	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	e.setCmdEnv(cmd)

	return cmd.Run()
}

func (e *buildEnv) runCapture(arg0 string, args ...string) ([]byte, error) {
	log.Printf("build: running %s %s", arg0, strings.Join(args, " "))
	cmd := exec.Command(arg0, args...)

	cmd.Stderr = os.Stderr
	e.setCmdEnv(cmd)

	return cmd.Output()
}

func (e *buildEnv) getDir(name string) string {
	// return /pkg/main/${PKG}.core.${PVRF}
	return "/pkg/main/" + e.category + "." + e.name + "." + name + "." + e.pvrf
}

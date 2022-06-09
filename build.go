package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type buildEnv struct {
	backend   Backend
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
	Version   string   `yaml:"version"`
	Env       []string `yaml:"env,omitempty"`     // environment variables (using an array because order is important)
	Import    []string `yaml:"import,omitempty"`  // list of imports
	Source    []string `yaml:"source"`            // url of source (if multiple files, multiple urls)
	Patches   []string `yaml:"patches,omitempty"` // patches to apply to source
	Engine    string   `yaml:"engine,omitempty"`  // build engine
	Options   []string `yaml:"options,flow,omitempty"`
	Arguments []string `yaml:"arguments,omitempty"` // extra arguments

	ConfigurePre  []string `yaml:"configure_pre,omitempty"`
	ConfigurePost []string `yaml:"configure_post,omitempty"`
	CompilePre    []string `yaml:"compile_pre,omitempty"`
	CompilePost   []string `yaml:"compile_post,omitempty"`
	InstallPre    []string `yaml:"install_pre,omitempty"`
	InstallPost   []string `yaml:"install_post,omitempty"`
}

type buildConfig struct {
	pkgname  string
	epoch    string // unix timestamp of last commit of file
	meta     *buildMeta
	Versions *buildVersions        `yaml:"versions"`
	Build    []*buildInstructions  `yaml:"build"`
	Files    map[string]*buildFile `yaml:"files,omitempty"`
}

type buildMeta struct {
	Files map[string]*buildFile `yaml:"files"`
}

type buildFile struct {
	Size   int64
	Added  time.Time
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

func (bv *buildConfig) Export() (map[string][]byte, error) {
	build, err := yaml.Marshal(bv)
	if err != nil {
		return nil, err
	}
	meta, err := yaml.Marshal(bv.meta)
	if err != nil {
		return nil, err
	}

	return map[string][]byte{"build.yaml": build, "metadata.yaml": meta}, nil
}

func (bv *buildConfig) Save() error {
	data, err := bv.Export()
	if err != nil {
		return err
	}

	for fn, data := range data {
		err = ioutil.WriteFile(filepath.Join(repoPath(), bv.pkgname, fn), data, 0644)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *buildEnv) initVars() error {
	e.category = path.Dir(e.pkg.fn) // category, eg. app-arch
	e.name = path.Base(e.pkg.fn)    // zlib

	e.backend = NewLocal()

	err := e.initQemu()
	if err != nil {
		log.Printf("WARNING: failed to init qemu: %s (will build locally)", err)
	}

	tmpbase, err := e.backend.Base()
	if err != nil {
		return err
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

	log.Printf("Using %s as build directory", e.base)

	e.vars = map[string]string{
		"P":         e.name + "-" + e.version,
		"PN":        e.name,                   // zlib
		"PF":        e.name + "-" + e.version, // pf = full (includes revision)
		"CATEGORY":  e.category,
		"PV":        e.version,
		"PVR":       e.pvr, // TODO add revision (or strip from PV)
		"PVRF":      e.pvrf,
		"PKG":       e.category + "." + e.name,
		"WORKDIR":   e.workdir,
		"D":         e.dist,
		"T":         e.temp,
		"CHOST":     e.chost,
		"ARCH":      e.arch,
		"OS":        e.os,
		"BITS":      strconv.FormatInt(int64(e.bits), 10),
		"FILESDIR":  filepath.Join(repoPath(), e.config.pkgname, "files"),
		"LIBSUFFIX": e.libsuffix,

		// default stuff
		"PKG_CONFIG_LIBDIR": "/pkg/main/azusa.symlinks.core/pkgconfig",
		"XDG_DATA_DIRS":     "/usr/share",
		"SOURCE_DATE_EPOCH": e.config.epoch,
	}

	return nil
}

func (e *buildEnv) initDir() error {
	// cleanup
	e.backend.RemoveAll(e.base)
	err := e.backend.MkdirAll(e.base, 0755)
	if err != nil {
		return err
	}

	for _, sub := range []string{"work", "dist", "temp"} {
		err = e.backend.Mkdir(filepath.Join(e.base, sub), 0755)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *buildEnv) cleanup() error {
	return e.backend.RemoveAll(e.base)
}

func (e *buildEnv) getVar(v string) string {
	r, ok := e.vars[v]
	if ok {
		return r
	}
	return ""
}

func (e *buildEnv) appendVar(k, val, glue string) {
	r, ok := e.vars[k]
	if !ok {
		e.vars[k] = val
	} else {
		e.vars[k] = r + glue + val
	}
}

func (e *buildEnv) build(p *pkg) error {
	// let's just build latest version
	e.i = e.config.getInstructions(e.version)
	if e.i == nil {
		e.i = &buildInstructions{Engine: "auto"}
	}
	log.Printf("building version %s of %s using %s", e.version, p.fn, e.i.Engine)

	if err := e.initDir(); err != nil {
		return err
	}

	e.applyEnv()

	err := e.download()
	if err != nil {
		return err
	}

	err = e.applyPatches()
	if err != nil {
		return err
	}

	err = e.doImport()
	if err != nil {
		return err
	}

	// we call applyEnv twice because in some cases we use ${S} which is defined by e.download()
	e.applyEnv()

	if e.i.Engine == "auto" || e.i.Engine == "" {
		// detect based on files found in src
		if _, err = e.backend.Stat(filepath.Join(e.src, "CMakeLists.txt")); err == nil {
			e.i = &buildInstructions{Engine: "cmake"}
		} else if _, err = e.backend.Stat(filepath.Join(e.src, "meson_options.txt")); err == nil {
			e.i = &buildInstructions{Engine: "meson"}
		} else if _, err = e.backend.Stat(filepath.Join(e.src, "configure")); err == nil {
			e.i = &buildInstructions{Engine: "autoconf"}
		} else if _, err = e.backend.Stat(filepath.Join(e.src, "configure.ac")); err == nil {
			e.i = &buildInstructions{Engine: "autoconf", Options: []string{"autoreconf"}}
		} else {
			return errors.New("could not detect build type")
		}
	}

	// ok things are downloaded, now let's see what engine we're using
	switch e.i.Engine {
	case "autoconf":
		if err := e.buildAutoconf(); err != nil {
			return err
		}
	case "cmake":
		if err := e.buildCmake(); err != nil {
			return err
		}
	case "none":
		if err := e.buildNone(); err != nil {
			return err
		}
	case "meson":
		if err := e.buildMeson(); err != nil {
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
	e.cleanup()

	return nil
}

func (e *buildEnv) fullEnv() []string {
	var env []string

	env = append(env, "HOSTNAME=localhost", "HOME="+e.base, "PATH=/build/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	for k, v := range e.vars {
		env = append(env, k+"="+v)
	}

	return env
}

func (e *buildEnv) setCmdEnv(c *exec.Cmd) {
	c.Env = e.fullEnv()
}

func (e *buildEnv) run(args ...string) error {
	log.Printf("build: running %s", strings.Join(args, " "))

	return e.backend.RunEnv("/", args, e.fullEnv(), nil, nil)
}

func (e *buildEnv) runManyIn(dir string, cmds []string) error {
	for _, cmd := range cmds {
		err := e.runIn(dir, "/bin/bash", "-c", cmd)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *buildEnv) runIn(dir string, args ...string) error {
	log.Printf("build: running %s", strings.Join(args, " "))

	return e.backend.RunEnv(dir, args, e.fullEnv(), nil, nil)
}

func (e *buildEnv) runCapture(args ...string) ([]byte, error) {
	log.Printf("build: running %s", strings.Join(args, " "))

	buf := &bytes.Buffer{}
	err := e.backend.RunEnv("/", args, e.fullEnv(), buf, nil)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (e *buildEnv) runCaptureSilent(args ...string) ([]byte, error) {
	buf := &bytes.Buffer{}
	err := e.backend.RunEnv("/", args, e.fullEnv(), buf, io.Discard)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (e *buildEnv) getDir(name string) string {
	// return /pkg/main/${PKG}.core.${PVRF}
	return "/pkg/main/" + e.category + "." + e.name + "." + name + "." + e.pvrf
}

package main

import (
	"io/fs"
	"log"
	"path/filepath"
	"strings"
)

func (e *buildEnv) organize() error {
	if err := e.orgMoveLib(); err != nil {
		return err
	}
	if err := e.orgFixMultilib(); err != nil {
		return err
	}
	if err := e.orgMoveEtc(); err != nil {
		return err
	}
	if err := e.orgFixDev(); err != nil {
		return err
	}
	if err := e.orgFixDoc(); err != nil {
		return err
	}
	if err := e.orgFixUdev(); err != nil {
		return err
	}
	if err := e.orgFixFonts(); err != nil {
		return err
	}
	if err := e.orgFixPython(); err != nil {
		return err
	}
	return nil
}

func (e *buildEnv) orgMoveLib() error {
	log.Printf("Fixing libs...")
	// remove any .la file
	// see: https://wiki.gentoo.org/wiki/Project:Quality_Assurance/Handling_Libtool_Archives
	for _, p := range e.findFiles(e.dist, "*.la") {
		log.Printf("remove: $D/%s", p)
		e.Remove(filepath.Join(e.dist, p))
	}

	for _, sub := range []string{"lib", "lib32", "lib64"} {
		st, err := e.Lstat(filepath.Join(e.dist, e.getDir("core"), sub))
		if err != nil {
			continue
		}
		if st.Mode().Type() == fs.ModeSymlink {
			continue
		}

		err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), sub), filepath.Join(e.getDir("libs"), sub))
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *buildEnv) orgFixMultilib() error {
	if e.libsuffix == "" {
		return nil
	}

	for _, typ := range []string{"core", "libs", "dev"} {
		// if we have a "lib" dir and no lib64, move it
		_, errs := e.Lstat(filepath.Join(e.dist, e.getDir(typ), "lib"))
		if errs != nil {
			continue
		}
		_, errd := e.Lstat(filepath.Join(e.dist, e.getDir(typ), "lib"+e.libsuffix))
		if errd != nil {
			// source exists but not dest, move it
			log.Printf("rename %s to %s", filepath.Join(e.getDir(typ), "lib"), "lib"+e.libsuffix)
			e.Rename(filepath.Join(e.dist, e.getDir(typ), "lib"), filepath.Join(e.dist, e.getDir(typ), "lib"+e.libsuffix))
			e.Symlink("lib"+e.libsuffix, filepath.Join(e.dist, e.getDir(typ), "lib"))
		}
	}
	return nil
}

func (e *buildEnv) orgMoveEtc() error {
	if _, err := e.Stat(filepath.Join(e.dist, "etc")); err == nil {
		e.MkdirAll(filepath.Join(e.dist, e.getDir("core")), 0755)
		return e.Rename(filepath.Join(e.dist, "etc"), filepath.Join(e.dist, e.getDir("core"), "etc"))
	}
	return nil
}

func (e *buildEnv) orgFixDev() error {
	log.Printf("Running fixdev (moving development files like pkgconfig and cmake)...")

	for _, sub := range []string{"pkgconfig", "cmake"} {
		if st, err := e.Stat(filepath.Join(e.dist, e.getDir("libs"), "lib"+e.libsuffix, sub)); err == nil && st.IsDir() {
			// this should be moved to dev
			err = e.moveAndLinkDir(filepath.Join(e.getDir("libs"), "lib"+e.libsuffix, sub), filepath.Join(e.getDir("dev"), sub))
			if err != nil {
				return err
			}
		}
		if st, err := e.Stat(filepath.Join(e.dist, e.getDir("core"), "share", sub)); err == nil && st.IsDir() {
			// this should be moved to dev
			err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), "share", sub), filepath.Join(e.getDir("dev"), sub))
			if err != nil {
				return err
			}
		}
	}
	if st, err := e.Stat(filepath.Join(e.dist, e.getDir("core"), "include")); err == nil && st.IsDir() {
		// this should be in dev
		err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), "include"), filepath.Join(e.getDir("dev"), "include"))
		if err != nil {
			return err
		}
	}
	if st, err := e.Stat(filepath.Join(e.dist, e.getDir("libs"), "lib"+e.libsuffix)); err == nil && st.IsDir() {
		// locate any .a files
		list := e.findFiles(filepath.Join(e.dist, e.getDir("libs"), "lib"+e.libsuffix), "*.a")
		if len(list) > 0 {
			e.MkdirAll(filepath.Join(e.dist, e.getDir("dev"), "lib"+e.libsuffix), 0755)
			for _, f := range list {
				// need to move f to dev
				err = e.moveAndLinkFile(filepath.Join(e.getDir("libs"), "lib"+e.libsuffix, f), filepath.Join(e.getDir("dev"), "lib"+e.libsuffix, f))
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (e *buildEnv) orgFixDoc() error {
	for _, sub := range []string{"man", "info"} {
		if st, err := e.Stat(filepath.Join(e.dist, e.getDir("core"), sub)); err == nil && st.IsDir() {
			// this should be moved to doc
			err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), sub), filepath.Join(e.getDir("doc"), sub))
			if err != nil {
				return err
			}
		}
		if st, err := e.Stat(filepath.Join(e.dist, e.getDir("core"), "share", sub)); err == nil && st.IsDir() {
			// this should be moved to doc
			err = e.moveAndLinkDir(filepath.Join(e.getDir("core"), "share", sub), filepath.Join(e.getDir("doc"), sub))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *buildEnv) orgFixUdev() error {
	if _, err := e.Stat(filepath.Join(e.dist, "lib", "udev")); err == nil {
		// badly installed udev rules
		e.MkdirAll(filepath.Join(e.dist, e.getDir("core")), 0755)
		err = e.Rename(filepath.Join(e.dist, "lib", "udev"), filepath.Join(e.dist, e.getDir("core"), "udev"))
		if err != nil {
			return err
		}
		e.Remove(filepath.Join(e.dist, "lib"))
	}

	if st, err := e.Stat(filepath.Join(e.dist, e.getDir("libs"), "lib"+e.libsuffix, "udev")); err == nil && st.IsDir() {
		err = e.moveAndLinkDir(filepath.Join(e.getDir("libs"), "lib"+e.libsuffix, "udev"), filepath.Join(e.getDir("core"), "udev"))
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *buildEnv) orgFixFonts() error {
	// most packages will install fonts in /pkg/main/media-fonts.font-util.core*
	if e.category == "media-fonts" && e.name == "font-util" {
		// only apply this rule if not doing media-fonts/font-util
		return nil
	}
	sublist, err := e.ReadDir(filepath.Join(e.dist, "pkg", "main"))
	if err != nil {
		return err
	}
	for _, pkgInfo := range sublist {
		pkg := pkgInfo.Name()
		if strings.HasPrefix(pkg, "media-fonts.font-util.core") {
			if st, err := e.Stat(filepath.Join(e.dist, "pkg", "main", pkg, "share", "fonts")); err == nil && st.IsDir() {
				// this needs moving
				e.MkdirAll(filepath.Join(e.dist, e.getDir("fonts")), 0755)
				entries, err := e.ReadDir(filepath.Join(e.dist, "pkg", "main", pkg, "share", "fonts"))
				if err != nil {
					return err
				}
				for _, entry := range entries {
					ename := entry.Name()
					err = e.Rename(filepath.Join(e.dist, "pkg", "main", pkg, "share", "fonts", ename), filepath.Join(e.dist, e.getDir("fonts"), ename))
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (e *buildEnv) orgFixPython() error {
	if e.category == "dev-lang" && e.name == "python" {
		return nil
	}

	sublist, err := e.ReadDir(filepath.Join(e.dist, e.getDir("libs"), "lib"+e.libsuffix))
	if err != nil {
		// maybe just got no libs
		return nil
	}

	for _, subinfo := range sublist {
		sub := subinfo.Name()
		if !strings.HasPrefix(sub, "python") {
			continue
		}
		// this should be in a python module dir, not here. Let's try to find out what version of python this is and move it around.
		pyVerPrefix := strings.TrimPrefix(sub, "python")                          // eg. 3.10
		pyVer, err := e.Readlink("/pkg/main/dev-lang.python.core." + pyVerPrefix) // dev-lang.python.core.3.10.2.linux.amd64
		if err != nil {
			return err
		}
		pyVer = strings.TrimPrefix(pyVer, "dev-lang.python.core.") // 3.10.2.linux.amd64
		pyVer = trimOsArch(pyVer)
		e.MkdirAll(filepath.Join(e.dist, e.getDir("mod")+".py"+pyVer, "lib"), 0755)
		err = e.moveAndLinkDir(filepath.Join(e.getDir("libs"), "lib"+e.libsuffix, sub), filepath.Join(e.getDir("mod")+".py"+pyVer, "lib", sub))
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *buildEnv) moveAndLinkDir(src, dst string) error {
	log.Printf("Move & link %s to %s", src, dst)
	// src & dst start with "/pkg/main" - need to prepend e.dist if using
	if _, err := e.Stat(filepath.Join(e.dist, dst)); err != nil {
		err = e.MkdirAll(filepath.Join(e.dist, dst), 0755)
		if err != nil {
			return err
		}
	}
	list, err := e.ReadDir(filepath.Join(e.dist, src))
	if err != nil {
		return err
	}
	for _, fi := range list {
		nam := fi.Name()
		err = e.Rename(filepath.Join(e.dist, src, nam), filepath.Join(e.dist, dst, nam))
		if err != nil {
			return err
		}
	}
	// remove dir
	err = e.Remove(filepath.Join(e.dist, src))
	if err != nil {
		return err
	}
	// symlink to dst
	return e.Symlink(dst, filepath.Join(e.dist, src))
}

func (e *buildEnv) moveAndLinkFile(src, dst string) error {
	log.Printf("Move & link %s to %s", src, dst)
	dstdir := filepath.Dir(dst)
	// src & dst start with "/pkg/main" - need to prepend e.dist if using
	if _, err := e.Stat(filepath.Join(e.dist, dstdir)); err != nil {
		err = e.MkdirAll(filepath.Join(e.dist, dstdir), 0755)
		if err != nil {
			return err
		}
	}
	err := e.Rename(filepath.Join(e.dist, src), filepath.Join(e.dist, dst))
	if err != nil {
		return err
	}
	// symlink to dst
	return e.Symlink(dst, filepath.Join(e.dist, src))
}

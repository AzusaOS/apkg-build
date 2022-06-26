package main

import (
	"bytes"
	"strings"
)

func (e *buildEnv) doImport() error {
	// read e.i.Import, for each line modify CPPFLAGS and LDFLAGS
	var pkgconfig []string

	// TODO support "X"

	for _, s := range e.i.Import {
		p := strings.IndexByte(s, '/')
		if p == -1 {
			// pkg-config package
			pkgconfig = append(pkgconfig, s)
			continue
		}
		p = strings.IndexByte(s, ':')
		vers := ""
		if p != -1 {
			// got a version definition
			vers = "." + s[p+1:]
			s = s[:p]
		}
		vers = vers + "." + e.os + "." + e.arch

		s = strings.ReplaceAll(s, "/", ".")
		incDir := "/pkg/main/" + s + ".dev" + vers + "/include"
		libDir := "/pkg/main/" + s + ".libs" + vers + "/lib" + e.libsuffix

		if _, err := e.backend.Stat(incDir); err == nil {
			// found includes
			e.appendVar("CPPFLAGS", "-I"+incDir, " ")
			e.appendVar("CPATH", incDir, ":")
			e.appendVar("CMAKE_SYSTEM_INCLUDE_PATH", incDir, ";")
		}
		if _, err := e.backend.Stat(libDir); err == nil {
			e.appendVar("LDFLAGS", "-L"+libDir, " ")
			e.appendVar("CMAKE_SYSTEM_LIBRARY_PATH", libDir, ";")
		}
	}

	if len(pkgconfig) > 0 {
		// run pkgconfig
		err := e.run(append([]string{"pkg-config", "--exists", "--print-errors"}, pkgconfig...)...)
		if err != nil {
			return err
		}
		data, err := e.runCapture(append([]string{"pkg-config", "--cflags-only-I"}, pkgconfig...)...)
		if err != nil {
			return err
		}
		data = bytes.TrimSpace(data)
		e.appendVar("CPPFLAGS", string(data), " ")
		vals := bytes.Split(data, []byte{' '})
		for _, v := range vals {
			if len(v) == 0 {
				continue
			}
			s := strings.TrimPrefix(string(v), "-I")
			e.appendVar("CPATH", s, ":")
			e.appendVar("CMAKE_SYSTEM_INCLUDE_PATH", s, ";")
		}

		data, err = e.runCapture(append([]string{"pkg-config", "--libs-only-L"}, pkgconfig...)...)
		if err != nil {
			return err
		}
		data = bytes.TrimSpace(data)
		e.appendVar("LDFLAGS", string(data), " ")
		vals = bytes.Split(data, []byte{' '})
		for _, v := range vals {
			if len(v) == 0 {
				continue
			}
			s := strings.TrimPrefix(string(v), "-L")
			e.appendVar("CMAKE_SYSTEM_LIBRARY_PATH", s, ";")
		}
	}
	return nil
}

package main

import (
	"fmt"
	"path/filepath"

	"mvdan.cc/sh/v3/shell"
)

func (e *buildEnv) buildCmake() error {
	// build custom rules (gentoo inspired)
	buildRules := filepath.Join(e.base, "azusa_rules.cmake")

	f, err := e.backend.Create(buildRules)
	if err != nil {
		return err
	}

	// allow override of cmakeRoot via CMAKE_ROOT
	cmakeRoot := e.src
	if v := e.getVar("CMAKE_ROOT"); v != "" {
		cmakeRoot = v
	}

	cppFlags := e.getVar("CPPFLAGS")

	fmt.Fprintf(f, `set(CMAKE_ASM_COMPILE_OBJECT "<CMAKE_ASM_COMPILER> <DEFINES> <INCLUDES> %s <FLAGS> -o <OBJECT> -c <SOURCE>" CACHE STRING "ASM compile command" FORCE)`+"\n", cppFlags)
	fmt.Fprintf(f, `set(CMAKE_ASM-ATT_COMPILE_OBJECT "<CMAKE_ASM-ATT_COMPILER> <DEFINES> <INCLUDES> %s <FLAGS> -o <OBJECT> -c -x assembler <SOURCE>" CACHE STRING "ASM-ATT compile command" FORCE)`+"\n", cppFlags)
	fmt.Fprintf(f, `set(CMAKE_ASM-ATT_LINK_FLAGS "-nostdlib" CACHE STRING "ASM-ATT link flags" FORCE)`+"\n")
	fmt.Fprintf(f, `set(CMAKE_C_COMPILE_OBJECT "<CMAKE_C_COMPILER> <DEFINES> <INCLUDES> %s <FLAGS> -o <OBJECT> -c <SOURCE>" CACHE STRING "C compile command" FORCE)`+"\n", cppFlags)
	fmt.Fprintf(f, `set(CMAKE_CXX_COMPILE_OBJECT "<CMAKE_CXX_COMPILER> <DEFINES> <INCLUDES> %s <FLAGS> -o <OBJECT> -c <SOURCE>" CACHE STRING "C++ compile command" FORCE)`+"\n", cppFlags)
	fmt.Fprintf(f, `set(CMAKE_Fortran_COMPILE_OBJECT "<CMAKE_Fortran_COMPILER> <DEFINES> <INCLUDES> %s <FLAGS> -o <OBJECT> -c <SOURCE>" CACHE STRING "Fortran compile command" FORCE)`+"\n", e.getVar("FCFLAGS"))
	f.Close()

	commonConfig := filepath.Join(e.base, "azusa_common_config.cmake")

	f, err = e.backend.Create(commonConfig)
	if err != nil {
		return err
	}

	fmt.Fprintf(f, `set(LIB_SUFFIX "%s" CACHE STRING "library path suffix" FORCE)`+"\n", e.libsuffix)
	fmt.Fprintf(f, `set(CMAKE_INSTALL_BINDIR "%s/bin" CACHE PATH "")`+"\n", e.getDir("core"))
	fmt.Fprintf(f, `set(CMAKE_INSTALL_DATADIR "%s/share" CACHE PATH "")`+"\n", e.getDir("core"))
	fmt.Fprintf(f, `set(CMAKE_INSTALL_LIBDIR "%s/lib%s" CACHE PATH "Output directory for libraries")`+"\n", e.getDir("libs"), e.libsuffix)
	fmt.Fprintf(f, `set(CMAKE_INSTALL_DOCDIR "%s" CACHE PATH "")`+"\n", e.getDir("doc"))
	fmt.Fprintf(f, `set(CMAKE_INSTALL_INFODIR "%s/info" CACHE PATH "")`+"\n", e.getDir("doc"))
	fmt.Fprintf(f, `set(CMAKE_INSTALL_MANDIR "%s/man" CACHE PATH "")`+"\n", e.getDir("doc"))
	fmt.Fprintf(f, `set(CMAKE_USER_MAKE_RULES_OVERRIDE "%s" CACHE FILEPATH "Azusa override rules")`+"\n", buildRules)
	fmt.Fprintf(f, `set(BUILD_SHARED_LIBS ON CACHE BOOL "")`+"\n")
	f.Close()

	// for kde's extra-cmake-modules
	e.vars["ECM_DIR"] = "/pkg/main/kde-frameworks.extra-cmake-modules.core/share/ECM/cmake"

	cmakeOpts := []string{
		"cmake",
		cmakeRoot,
		"-C", commonConfig,
		"-G", "Ninja", "-Wno-dev",
		"-DCMAKE_INSTALL_PREFIX=" + e.getDir("core"),
		"-DCMAKE_BUILD_TYPE=Release",
		"-DBUILD_SHARED_LIBS=ON",
		"-DCMAKE_SYSTEM_INCLUDE_PATH=" + e.getVar("CMAKE_SYSTEM_INCLUDE_PATH"),
		"-DCMAKE_SYSTEM_LIBRARY_PATH=" + e.getVar("CMAKE_SYSTEM_LIBRARY_PATH"),
		"-DCMAKE_C_FLAGS=" + cppFlags,
		"-DCMAKE_CXX_FLAGS=" + cppFlags,
	}

	buildDir := e.temp

	for _, arg := range e.i.Arguments {
		arg, err = shell.Expand(arg, e.getVar)
		if err != nil {
			return err
		}
		cmakeOpts = append(cmakeOpts, arg)
	}

	err = e.runManyIn(buildDir, e.i.ConfigurePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, cmakeOpts...)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.ConfigurePost)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "ninja")
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.CompilePost)
	if err != nil {
		return err
	}

	// let cmake know of our DESTDIR
	e.vars["DESTDIR"] = e.dist

	err = e.runManyIn(buildDir, e.i.InstallPre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, "ninja", "install")
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPost)
	if err != nil {
		return err
	}

	return nil
}

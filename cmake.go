package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func (e *buildEnv) buildCmake() error {
	// build custom rules (gentoo inspired)
	buildRules := filepath.Join(e.base, "azusa_rules.cmake")

	f, err := os.Create(buildRules)
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

	f, err = os.Create(commonConfig)
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

	err = e.runIn(e.temp, cmakeOpts[0], cmakeOpts[1:]...)
	if err != nil {
		return err
	}

	err = e.runIn(e.temp, "ninja")
	if err != nil {
		return err
	}

	e.vars["DESTDIR"] = e.dist
	err = e.runIn(e.temp, "ninja", "install")
	if err != nil {
		return err
	}

	return nil
}

package main

func (e *buildEnv) buildMeson() error {
	// build custom rules (gentoo inspired)

	// allow override of mesonRoot via MESON_ROOT
	mesonRoot := e.src
	if v := e.getVar("MESON_ROOT"); v != "" {
		mesonRoot = v
	}

	mesonOpts := []string{
		"meson",
		mesonRoot,
		"--prefix=" + e.getDir("core"),
		"-Dbuildtype=release",
	}

	buildDir := e.temp

	err := e.runManyIn(buildDir, e.i.ConfigurePre)
	if err != nil {
		return err
	}

	err = e.runIn(buildDir, mesonOpts...)
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

	// let meson know of our DESTDIR
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

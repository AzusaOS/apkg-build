package main

func (e *buildEnv) buildNone() error {
	buildDir := e.src

	err := e.runManyIn(buildDir, e.i.ConfigurePre)
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

	err = e.runManyIn(buildDir, e.i.CompilePost)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPre)
	if err != nil {
		return err
	}

	err = e.runManyIn(buildDir, e.i.InstallPost)
	if err != nil {
		return err
	}

	return nil
}

package main

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type localBackend struct{}

func NewLocal() Backend {
	return &localBackend{}
}

func (b *localBackend) Base() (string, error) {
	if err := unix.Access("/build", unix.W_OK); err == nil {
		return "/build", nil
	}
	// can't use /build
	home := os.Getenv("HOME")
	if home == "" {
		return fmt.Sprintf("/tmp/build-%d", os.Getuid()), nil
	}
	return filepath.Join(home, "tmp", "build"), nil
}

func (b *localBackend) IsLocal() bool {
	return true
}

func (b *localBackend) IsRoot() bool {
	return os.Getuid() == 0
}

func (b *localBackend) Create(p string) (io.WriteCloser, error) {
	return os.Create(p)
}

func (b *localBackend) Stat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}

func (b *localBackend) Lstat(p string) (os.FileInfo, error) {
	return os.Lstat(p)
}

func (b *localBackend) Mkdir(dir string, mode fs.FileMode) error {
	return os.Mkdir(dir, mode)
}

func (b *localBackend) MkdirAll(dir string, mode fs.FileMode) error {
	return os.MkdirAll(dir, mode)
}

func (b *localBackend) ReadDir(p string) ([]os.FileInfo, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdir(0)
}

func (b *localBackend) Readlink(p string) (string, error) {
	return os.Readlink(p)
}

func (b *localBackend) Remove(p string) error {
	return os.Remove(p)
}

func (b *localBackend) RemoveAll(p string) error {
	return os.RemoveAll(p)
}

func (b *localBackend) Rename(oldname, newname string) error {
	return os.Rename(oldname, newname)
}

func (b *localBackend) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (b *localBackend) ReadFile(p string) ([]byte, error) {
	return ioutil.ReadFile(p)
}

func (b *localBackend) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	return ioutil.WriteFile(filename, data, perm)
}

func (b *localBackend) ImportFile(tgt, src string) error {
	if src == tgt {
		return nil
	}
	return cloneFile(tgt, src)
}

func (b *localBackend) ExportFile(src, tgt string) error {
	if src == tgt {
		return nil
	}
	return cloneFile(tgt, src)
}

func (b *localBackend) RunEnv(dir string, args []string, env []string, stdout, stderr io.Writer) error {
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	c.Env = env

	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	c.Stdout = stdout
	c.Stderr = stderr

	return c.Run()
}

func (b *localBackend) FindFiles(dir string, fnList ...string) []string {
	return findFiles(dir, fnList...)
}

func (b *localBackend) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)

}

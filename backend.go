package main

import (
	"io"
	"io/fs"
	"net"
	"os"
)

// Backend are basically build environments
type Backend interface {
	Base() (string, error)
	IsLocal() bool
	IsRoot() bool
	RunEnv(dir string, args []string, env []string, stdout, stderr io.Writer) error
	MkdirAll(dir string, mode fs.FileMode) error
	Mkdir(dir string, mode fs.FileMode) error
	ReadFile(filename string) ([]byte, error)
	WriteFile(filename string, data []byte, perm fs.FileMode) error
	Stat(p string) (os.FileInfo, error)
	Lstat(p string) (os.FileInfo, error)
	Readlink(p string) (string, error)
	ReadDir(p string) ([]os.FileInfo, error)
	Rename(oldname, newname string) error
	Remove(f string) error
	RemoveAll(path string) error
	Symlink(oldname, newname string) error
	Create(f string) (io.WriteCloser, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
	FindFiles(dir string, fnList ...string) []string // find files based on patterns (any matching file)
	PutFile(src, tgt string) error                   // copy local file src to remote target tgt
	GetFile(remote, local string) error              // copy remote file to local
	Close() error                                    // cleanup resources

	Dial(n, addr string) (net.Conn, error) // dial inside backend
}

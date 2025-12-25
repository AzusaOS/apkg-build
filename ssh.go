package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshBackend struct {
	ssh      *ssh.Client
	sftp     *sftp.Client
	useProxy bool
	uid      int
}

func NewSshBackend(s *ssh.Client) (Backend, error) {
	sess, err := s.NewSession()
	if err != nil {
		return nil, err
	}
	res, err := sess.Output("uname -a")
	sess.Close()
	if err != nil {
		return nil, err
	}
	log.Printf("ssh: ready, running %s", bytes.TrimSpace(res))

	ftp, err := sftp.NewClient(s)
	if err != nil {
		return nil, err
	}

	b := &sshBackend{
		ssh:  s,
		sftp: ftp,
		uid:  -1,
	}

	if _, err := ftp.Stat("/pkg/main/sys-process.execproxy.core/libexec/execproxy"); err == nil {
		b.useProxy = true
	}

	idS, err := b.runCapture("/usr/bin/id", "-u")
	if err == nil {
		id, err := strconv.ParseInt(strings.TrimSpace(string(idS)), 10, 64)
		if err == nil {
			b.uid = int(id)
		} else {
			log.Printf("ssh: failed to parse uid %s: %s", idS, err)
			return nil, err
		}
	} else {
		log.Printf("ssh: failed to get connected ID: %s", err)
	}
	log.Printf("ssh: running with uid=%d", b.uid)

	return b, nil
}

func (b *sshBackend) Base() (string, error) {
	if b.uid == 0 {
		return "/build", nil
	}

	// just return something in /tmp instead of grabbing $HOME
	return fmt.Sprintf("/tmp/build-%d", b.uid), nil
}

func (b *sshBackend) IsLocal() bool {
	return false
}

func (b *sshBackend) IsRoot() bool {
	return b.uid == 0
}

func (b *sshBackend) Create(p string) (io.WriteCloser, error) {
	return b.sftp.Create(p)
}

func (b *sshBackend) Stat(p string) (os.FileInfo, error) {
	return b.sftp.Stat(p)
}

func (b *sshBackend) Lstat(p string) (os.FileInfo, error) {
	return b.sftp.Lstat(p)
}

func (b *sshBackend) Mkdir(dir string, mode fs.FileMode) error {
	return b.sftp.Mkdir(dir)
}

func (b *sshBackend) MkdirAll(dir string, mode fs.FileMode) error {
	return b.sftp.MkdirAll(dir)
}

func (b *sshBackend) ReadDir(p string) ([]os.FileInfo, error) {
	return b.sftp.ReadDir(p)
}

func (b *sshBackend) Readlink(p string) (string, error) {
	return b.sftp.ReadLink(p)
}

func (b *sshBackend) Remove(p string) error {
	return b.sftp.Remove(p)
}

func (b *sshBackend) RemoveAll(p string) error {
	return b.RunEnv("/", []string{"rm", "-fr", p}, nil, nil, nil)
}

func (b *sshBackend) Rename(oldname, newname string) error {
	return b.sftp.Rename(oldname, newname)
}

func (b *sshBackend) Symlink(oldname, newname string) error {
	return b.sftp.Symlink(oldname, newname)
}

func (b *sshBackend) ReadFile(p string) ([]byte, error) {
	f, err := b.sftp.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

func (b *sshBackend) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	f, err := b.sftp.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Chmod(perm)

	_, err = f.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func (b *sshBackend) PutFile(src, tgt string) error {
	log.Printf("Copying local file %s to %s", src, tgt)
	// need to create file via sftp
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := b.sftp.Create(tgt)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	if st, err := in.Stat(); err == nil {
		// chmod
		out.Chmod(st.Mode())
	}
	return nil
}

func (b *sshBackend) GetFile(remote, local string) error {
	log.Printf("qemu: copying remote %s to local %s", remote, local)
	in, err := b.sftp.Open(remote)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(local)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	if st, err := in.Stat(); err == nil {
		out.Chmod(st.Mode())
	}
	return nil
}

func (b *sshBackend) runCapture(args ...string) ([]byte, error) {
	buf := &bytes.Buffer{}
	err := b.RunEnv("/", args, nil, buf, nil)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *sshBackend) RunEnv(dir string, args []string, env []string, stdout, stderr io.Writer) error {
	sess, err := b.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	if env == nil {
		env = []string{"HOME=/", "PATH=/build/bin:/sbin:/bin"}
		//env = os.Environ()
	}

	// copy env
	// looks like setenv doesn't work with dropbear
	/*
		for _, e := range env {
			p := strings.IndexByte(e, '=')
			if p != -1 {
				err = sess.Setenv(e[:p], e[p+1:])
				if err != nil {
					log.Printf("error: %s", err)
				}
			}
		}*/

	pipeout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	pipeerr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	go io.Copy(stdout, pipeout)
	go io.Copy(stderr, pipeerr)

	if b.useProxy {
		// let's use proxy mode
		proxy := "/pkg/main/sys-process.execproxy.core/libexec/execproxy"
		env = append(env, "PWD="+dir) // add pwd to envp

		// generate buffer
		buf := &bytes.Buffer{}
		buf.Write([]byte{0, 0, 0, 0}) // this will contain the len at the end
		buf.WriteByte(byte(len(args)))
		buf.WriteByte(byte(len(env)))
		buf.WriteByte(0) // do not give full path, let execproxy find it
		for _, s := range args {
			buf.WriteString(s)
			buf.WriteByte(0)
		}
		for _, s := range env {
			buf.WriteString(s)
			buf.WriteByte(0)
		}
		// make final buffer
		v := buf.Bytes()
		binary.BigEndian.PutUint32(v[:4], uint32(len(v)-4))

		// do it
		pipein, err := sess.StdinPipe()
		if err != nil {
			return err
		}
		go func() {
			defer pipein.Close()
			io.Copy(pipein, bytes.NewReader(v))
		}()

		return sess.Run(proxy)
	}

	return sess.Run(shellQuoteCmd("cd", dir) + ";" + shellQuoteEnv(env...) + shellQuoteCmd(args...))
}

func (b *sshBackend) FindFiles(dir string, fnList ...string) []string {
	dir = filepath.Clean(dir)

	cmd := []string{"find", dir, "-type", "f", "("}

	for i, fn := range fnList {
		if i == 0 {
			cmd = append(cmd, "-name", fn)
		} else {
			cmd = append(cmd, "-o", "-name", fn)
		}
	}
	cmd = append(cmd, ")", "-print0")

	res, err := b.runCapture(cmd...)
	if err != nil {
		return nil
	}
	vs := bytes.Split(res, []byte{0})
	// typically, last vs should be empty
	if len(vs[len(vs)-1]) == 0 {
		vs = vs[:len(vs)-1]
	}
	if len(vs) == 0 {
		return nil
	}

	final := make([]string, len(vs))
	for i, a := range vs {
		s := string(a)
		if p, err := filepath.Rel(dir, s); err == nil {
			s = p
		} else {
			s = strings.TrimPrefix(s, dir)
		}
		final[i] = s
	}
	return final
}

func (b *sshBackend) WalkDir(root string, fn fs.WalkDirFunc) error {
	walk := b.sftp.Walk(root)
	for walk.Step() {
		if err := walk.Err(); err != nil {
			return err
		}
		p := walk.Path()
		// func(path string, d fs.DirEntry, err error) error
		// stat it
		st, err := b.Stat(p)
		if err != nil {
			return err
		}
		err = fn(p, statThing{st}, nil)
		if err != nil {
			return err
		}
	}
	return nil

}

type statThing struct {
	os.FileInfo
}

func (s statThing) Info() (os.FileInfo, error) {
	return s.FileInfo, nil
}

func (s statThing) Type() fs.FileMode {
	return s.Mode().Type()
}

func (b *sshBackend) Dial(n, addr string) (net.Conn, error) {
	return b.ssh.Dial(n, addr)
}

func (b *sshBackend) Close() error {
	if b.sftp != nil {
		b.sftp.Close()
	}
	if b.ssh != nil {
		b.ssh.Close()
	}
	return nil
}

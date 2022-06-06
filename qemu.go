package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type qemu struct {
	cmd  *exec.Cmd
	ssh  *ssh.Client
	sftp *sftp.Client
	port int
}

func (e *buildEnv) initQemu() error {
	// launch qemu... first we need to find out kernel version
	kverB, err := ioutil.ReadFile("/pkg/main/sys-kernel.linux.core/version.txt")
	if err != nil {
		return err
	}
	kver := strings.TrimSpace(string(kverB))
	log.Printf("qemu: running with kernel %s", kver)

	// let's try to locate initrd for this kernel
	initrd := fmt.Sprintf("/tmp/initrd-apkg-build.kernel.%s.img", kver)
	if _, err := os.Stat(initrd); err != nil {
		// we need to create initrd
		log.Printf("Creating %s ...", initrd)
		cpio := fmt.Sprintf("/tmp/initrd-apkg-build.kernel.%s.cpio", kver)
		c := exec.Command("/bin/bash", "-c", "find . | cpio -H newc -o -R +0:+0 -V --file "+cpio)
		c.Dir = "/pkg/main/sys-kernel.linux.modules." + kver
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		err = c.Run()
		if err != nil {
			return err
		}

		// let's use /tmp
		os.MkdirAll("/tmp/usr/azusa", 0755)
		err = cloneFile("/tmp/usr/azusa/busybox", "/pkg/main/sys-apps.busybox.core/bin/busybox")
		if err != nil {
			return err
		}
		err = cloneFile("/tmp/usr/azusa/simple.script", "/pkg/main/sys-apps.busybox.doc/examples/udhcp/simple.script")
		if err != nil {
			return err
		}
		err = cloneFile("/tmp/usr/azusa/apkg", "/pkg/main/azusa.apkg.core/apkg")
		if err != nil {
			return err
		}
		str := strings.ReplaceAll(initData, "__ARCH__", e.arch)
		err = ioutil.WriteFile("/tmp/init", []byte(str), 0755)
		if err != nil {
			return err
		}

		// update cpio
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "usr\nusr/azusa\nusr/azusa/busybox\nusr/azusa/simple.script\nusr/azusa/apkg\ninit\n")

		c = exec.Command("cpio", "-H", "newc", "-o", "-R", "+0:+0", "-V", "--append", "--file", cpio)
		c.Dir = "/tmp"
		c.Stdin = bytes.NewReader(buf.Bytes())
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		err = c.Run()
		if err != nil {
			return err
		}

		// cleanup (TODO use a temp subdir for that)
		os.Remove("/tmp/init")
		os.RemoveAll("/tmp/usr")

		// compress
		c = exec.Command("xz", "-v", "--check=crc32", "--x86", "--lzma2", "--stdout", cpio)
		out, err := os.Create(initrd)
		if err != nil {
			return err
		}
		c.Stdout = out
		c.Stderr = os.Stderr
		err = c.Run()
		out.Close()
		if err != nil {
			return err
		}

		os.Remove(cpio)
	}

	qemuExe := ""
	switch e.arch {
	case "amd64":
		qemuExe = "qemu-system-x86_64"
	case "386":
		qemuExe = "qemu-system-x86_64"
	default:
		return fmt.Errorf("qemu arch not supported: %s", e.arch)
	}

	// choose a random port
	port := rand.Intn(10000) + 10000
	log.Printf("qemu: using qemu %s port %d for SSH", qemuExe, port)

	c := exec.Command(
		"/pkg/main/app-emulation.qemu.core/bin/"+qemuExe,
		"-kernel", "/pkg/main/sys-kernel.linux.core."+kver+"/linux-"+kver+".img",
		"-initrd", initrd,
		//"-append", "console=ttyS0",
		//"-serial", "stdio", // exclusive with -nographic
		"-m", "8192",
		"-cpu", "host",
		"--enable-kvm",
		"-netdev", fmt.Sprintf("user,id=hostnet0,hostfwd=tcp:127.0.0.1:%d-:22", port),
		"-device", "e1000,netdev=hostnet0",
	)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	c.Start()

	// let's try to connect to this port
	cfg := &ssh.ClientConfig{
		User: "root",
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// accept anything
			return nil
		},
		Timeout: 10 * time.Second,
	}

	log.Printf("Waiting for qemu to finish loading...")

	var sshc *ssh.Client
	for {
		sshc, err = ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		break
	}

	sess, err := sshc.NewSession()
	if err != nil {
		return err
	}
	res, err := sess.Output("uname -a")
	sess.Close()
	if err != nil {
		return err
	}
	log.Printf("qemu: ready, running %s", bytes.TrimSpace(res))

	ftp, err := sftp.NewClient(sshc)
	if err != nil {
		return err
	}

	e.qemu = &qemu{
		cmd:  c,
		ssh:  sshc,
		sftp: ftp,
		port: port,
	}

	return nil
}

func (q *qemu) runEnv(dir string, args []string, env []string, stdout, stderr io.Writer) error {
	sess, err := q.ssh.NewSession()
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

	// copy env
	for _, e := range env {
		p := strings.IndexByte(e, '=')
		if p != -1 {
			sess.Setenv(e[:p], e[p+1:])
		}
	}

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

	return sess.Run(shellQuoteCmd("cd", dir) + ";" + shellQuoteCmd(args...))
}

func (q *qemu) run(args ...string) error {
	sess, err := q.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	return sess.Run(shellQuoteCmd(args...))
}

func shellQuoteCmd(args ...string) string {
	cmd := &bytes.Buffer{}
	for _, arg := range args {
		if cmd.Len() > 0 {
			cmd.WriteByte(' ')
		}
		cmd.WriteString(shellQuote(arg))
	}
	return cmd.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

const initData = `#!/usr/azusa/busybox ash

mkdir /bin /sbin
/usr/azusa/busybox --install

mkdir /proc
mount -t proc proc /proc

mkdir -p /etc
ln -snf /proc/self/mounts /etc/mtab

mkdir -p /sys
mount -t sysfs sys /sys
mkdir -p /tmp /var/log

# init /dev (on ramfs)
mkdir -p /dev
if [ ! -e /dev/console ]; then
	mknod /dev/console c 5 1
fi
mknod /dev/full c 1 7
mknod /dev/kmem c 1 2
mknod /dev/mem c 1 1
mknod /dev/null c 1 3
mknod /dev/port c 1 4
mknod /dev/random c 1 8
mknod /dev/urandom c 1 9
mknod /dev/zero c 1 5
mount -t devtmpfs dev /dev
mkdir /dev/pts
mount -t devpts devpts /dev/pts

find /sys -name modalias -print0 | xargs -0 sort -u | xargs /sbin/modprobe -a

ip link set lo up
ip link set eth0 up
udhcpc -n -i eth0 -s /usr/azusa/simple.script

modprobe fuse
/usr/azusa/apkg >/var/log/apkg.log 2>&1 &

# wait for /pkg/main to be ready
echo "Waiting..."
while true; do
	sleep 1
	if [ -d /pkg/main/azusa.symlinks.core/ ]; then
		break
	fi
done

# rely on busybox for the next lines...
rm -fr /bin /sbin
ln -snf /pkg/main/azusa.symlinks.core.linux.amd64/bin /bin
ln -snf /pkg/main/azusa.symlinks.core.linux.amd64/sbin /sbin
hash -r
export PATH=/sbin:/bin

mkdir -p /usr/libexec
ln -snf /pkg/main/net-misc.openssh.core/libexec/sftp-server /usr/libexec

/bin/find /pkg/main/azusa.baselayout.core/ '(' -type f -o -type l ')' -printf '%P\n' | while read foo; do
	if [ ! -f "$foo" ]; then
		foo_dir="$(dirname "$foo")"
		if [ ! -d "$foo_dir" ]; then
			# make dir if missing
			mkdir -p "$BASE/$foo_dir"
		fi
		cp -a "/pkg/main/azusa.baselayout.core/$foo" "$BASE/$foo"
	fi
done

dbus-uuidgen --ensure=/etc/machine-id

# set root password to empty
sed -i 's/root:\*:/root::/' /etc/shadow

echo "Running dropbear..."
mkdir /etc/dropbear
dropbear -E -B -R

/bin/bash -i
poweroff -f
`

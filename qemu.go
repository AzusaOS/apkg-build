package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func (e *buildEnv) initQemu() error {
	qemuExe := ""
	qemuMachine := ""
	var port int
	switch e.arch {
	case "amd64":
		qemuExe = "qemu-system-x86_64"
		qemuMachine = "q35"
		port = 10088
	case "386":
		qemuExe = "qemu-system-x86_64"
		qemuMachine = "q35"
		port = 10089
	case "arm64":
		qemuExe = "qemu-system-aarch64"
		qemuMachine = "virt"
		port = 10090
	default:
		return fmt.Errorf("qemu arch not supported: %s", e.arch)
	}

	// let's try once to see if qemu is out there

	cfg := &ssh.ClientConfig{
		User: "root",
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// accept anything
			return nil
		},
		Timeout: 10 * time.Second,
	}

	sshc, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
	if err == nil {
		// ooh, let's try that
		be, err := NewSshBackend(sshc)
		if err == nil {
			e.backend = be
			return nil
		}
	}

	// launch qemu... first we need to find out kernel version
	kverB, err := ioutil.ReadFile("/pkg/main/sys-kernel.linux.core." + e.os + "." + e.arch + "/version.txt")
	if err != nil {
		return err
	}
	kver := strings.TrimSpace(string(kverB))
	log.Printf("qemu: running with kernel %s", kver)

	// let's try to locate initrd for this kernel
	initrd := fmt.Sprintf("/tmp/initrd-apkg-build.kernel."+e.arch+".%s.img", kver)
	if _, err := os.Stat(initrd); err != nil {
		// we need to create initrd
		log.Printf("Creating %s ...", initrd)
		cpio := fmt.Sprintf("/tmp/initrd-apkg-build.kernel."+e.arch+".%s.cpio", kver)
		c := exec.Command("/bin/bash", "-c", "find . | cpio -H newc -o -R +0:+0 -V --file "+cpio)
		c.Dir = "/pkg/main/sys-kernel.linux.modules." + kver + "." + e.os + "." + e.arch
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		err = c.Run()
		if err != nil {
			return err
		}

		// let's use /tmp
		os.MkdirAll("/tmp/usr/azusa", 0755)
		err = cloneFile("/pkg/main/sys-apps.busybox.core."+e.os+"."+e.arch+"/bin/busybox", "/tmp/usr/azusa/busybox")
		if err != nil {
			return err
		}
		err = cloneFile("/pkg/main/sys-apps.busybox.doc."+e.os+"."+e.arch+"/examples/udhcp/simple.script", "/tmp/usr/azusa/simple.script")
		if err != nil {
			return err
		}
		err = cloneFile("/pkg/main/azusa.apkg.core."+e.os+"."+e.arch+"/apkg", "/tmp/usr/azusa/apkg")
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

	log.Printf("qemu: using qemu %s port %d for SSH", qemuExe, port)

	qemuCmd := []string{
		"/pkg/main/app-emulation.qemu.core/bin/" + qemuExe,
		"-kernel", "/pkg/main/sys-kernel.linux.core." + kver + "." + e.os + "." + e.arch + "/linux-" + kver + ".img",
		"-initrd", initrd,
		//"-append", "console=ttyS0",
		//"-serial", "stdio", // exclusive with -nographic
		"-M", qemuMachine,
		"-netdev", fmt.Sprintf("user,id=hostnet0,hostfwd=tcp:127.0.0.1:%d-:22", port),
		"-device", "e1000,netdev=hostnet0",
	}
	switch e.arch {
	case "amd64", "386":
		qemuCmd = append(qemuCmd,
			"--enable-kvm",
			"-cpu", "host",
			"-smp", strconv.Itoa(runtime.NumCPU()),
			"-m", "8192",
		)
	case "arm64":
		qemuCmd = append(qemuCmd,
			"-cpu", "max", //"cortex-a53",
			"-smp", "4",
			//"-serial", "stdio",
			//"-append", "console=ttyS0",
			"-m", "2048",
		)
	}

	log.Printf("Running QEMU: %s", strings.Join(qemuCmd, " "))
	c := exec.Command(qemuCmd[0], qemuCmd[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	c.Start()

	// let's try to connect to this port
	log.Printf("Waiting for qemu to finish loading...")

	for {
		sshc, err = ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		be, err := NewSshBackend(sshc)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		e.backend = be
		return nil
	}
}

func shellQuoteEnv(env ...string) string {
	cmd := &bytes.Buffer{}
	for _, arg := range env {
		p := strings.IndexByte(arg, '=')
		if p == -1 {
			continue
		}
		cmd.WriteString(arg[:p+1] + shellQuote(arg[p+1:]))
		cmd.WriteByte(' ')
	}
	return cmd.String()
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

mkdir -p /var/lib/apkg
mkdir -p /build
mount -t tmpfs tmpfs /var/lib/apkg
mount -t tmpfs tmpfs /build
# disable ldconfig
mkdir /build/bin
echo '#!/bin/bash' >/build/bin/ldconfig
chmod +x /build/bin/ldconfig

modprobe fuse
/usr/azusa/apkg >/var/log/apkg.log 2>&1 &

# wait for /pkg/main to be ready
echo "Waiting..."
while true; do
	sleep 1
	if [ -d /pkg/main/azusa.symlinks.core.linux.__ARCH__/ ]; then
		break
	fi
done

# rely on busybox for the next lines...
rm -fr /bin /sbin
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/bin /bin
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/sbin /sbin
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/bin /usr/bin
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/sbin /usr/sbin
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/lib32 /lib32
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/lib64 /lib64
mv /lib /.lib.orig || true
ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/lib /lib


hash -r
export PATH=/sbin:/bin

mkdir -p /usr/libexec
ln -snf /pkg/main/net-misc.openssh.core.linux.__ARCH__/libexec/sftp-server /usr/libexec

/bin/find /pkg/main/azusa.baselayout.core.linux.__ARCH__/ '(' -type f -o -type l ')' -printf '%P\n' | while read foo; do
	if [ ! -f "$foo" ]; then
		foo_dir="$(dirname "$foo")"
		if [ ! -d "$foo_dir" ]; then
			# make dir if missing
			mkdir -p "$BASE/$foo_dir"
		fi
		cp -a "/pkg/main/azusa.baselayout.core.linux.__ARCH__/$foo" "$BASE/$foo"
	fi
done

dbus-uuidgen --ensure=/etc/machine-id

# set root password to empty
sed -i 's/root:\*:/root::/' /etc/shadow

echo "Running dropbear..."
mkdir /etc/dropbear
dropbear -E -B -R

/bin/bash -i || ash -i
poweroff -f
`

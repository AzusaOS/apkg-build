package main

import (
	"bytes"
	"fmt"
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

func NewQemuBackend(tgtos, arch string) (Backend, error) {
	qemuExe := ""
	qemuMachine := ""
	var port int
	switch arch {
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
		return nil, fmt.Errorf("qemu arch not supported: %s", arch)
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
		return NewSshBackend(sshc)
	}

	// launch qemu... first we need to find out kernel version
	kverB, err := os.ReadFile("/pkg/main/sys-kernel.linux.core." + tgtos + "." + arch + "/version.txt")
	if err != nil {
		return nil, err
	}
	kver := strings.TrimSpace(string(kverB))
	log.Printf("qemu: running with kernel %s", kver)

	// let's try to locate initrd for this kernel
	initrd := fmt.Sprintf("/tmp/initrd-apkg-build.kernel.%s.%s.img", arch, kver)
	if _, err := os.Stat(initrd); err != nil {
		// we need to create initrd
		log.Printf("Creating %s ...", initrd)
		cpio := fmt.Sprintf("/tmp/initrd-apkg-build.kernel.%s.%s.cpio", arch, kver)
		c := exec.Command("/bin/bash", "-c", "find . | cpio -H newc -o -R +0:+0 -V --file "+cpio)
		c.Dir = "/pkg/main/sys-kernel.linux.modules." + kver + "." + tgtos + "." + arch
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		err = c.Run()
		if err != nil {
			return nil, err
		}

		// let's use /tmp
		os.MkdirAll("/tmp/usr/azusa", 0755)
		err = cloneFile("/pkg/main/sys-apps.busybox.core."+tgtos+"."+arch+"/bin/busybox", "/tmp/usr/azusa/busybox")
		if err != nil {
			return nil, err
		}
		err = cloneFile("/pkg/main/sys-apps.busybox.doc."+tgtos+"."+arch+"/examples/udhcp/simple.script", "/tmp/usr/azusa/simple.script")
		if err != nil {
			return nil, err
		}
		err = cloneFile("/pkg/main/azusa.apkg.core."+tgtos+"."+arch+"/apkg", "/tmp/usr/azusa/apkg")
		if err != nil {
			return nil, err
		}
		str := strings.ReplaceAll(initData, "__ARCH__", arch)
		err = os.WriteFile("/tmp/init", []byte(str), 0755)
		if err != nil {
			return nil, err
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
			return nil, err
		}

		// cleanup (TODO use a temp subdir for that)
		os.Remove("/tmp/init")
		os.RemoveAll("/tmp/usr")

		// compress
		c = exec.Command("xz", "-v", "--check=crc32", "--x86", "--lzma2", "--stdout", cpio)
		out, err := os.Create(initrd)
		if err != nil {
			return nil, err
		}
		c.Stdout = out
		c.Stderr = os.Stderr
		err = c.Run()
		out.Close()
		if err != nil {
			return nil, err
		}

		os.Remove(cpio)
	}

	log.Printf("qemu: using qemu %s port %d for SSH", qemuExe, port)

	// create a disk image
	diskImage := fmt.Sprintf("/tmp/qemu-build-%s.qcow2", arch)
	os.Remove(diskImage)
	err = exec.Command("/pkg/main/app-emulation.qemu.core/bin/qemu-img", "create", "-f", "qcow2", diskImage, "128G").Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create build temp image: %w", err)
	}

	qemuCmd := []string{
		"/pkg/main/app-emulation.qemu.core/bin/" + qemuExe,
		"-kernel", "/pkg/main/sys-kernel.linux.core." + kver + "." + tgtos + "." + arch + "/linux-" + kver + ".img",
		"-initrd", initrd,
		//"-append", "console=ttyS0",
		//"-serial", "stdio", // exclusive with -nographic
		"-M", qemuMachine,
		"-netdev", fmt.Sprintf("user,id=hostnet0,hostfwd=tcp:127.0.0.1:%d-:22", port),
		"-device", "e1000,netdev=hostnet0",
		"-blockdev", "{\"driver\":\"file\",\"filename\":\"" + diskImage + "\",\"node-name\":\"build-storage\",\"discard\":\"unmap\"}",
		"-blockdev", `{"node-name":"build-format","read-only":false,"driver":"qcow2","file":"build-storage"}`,
		"-device", "ich9-ahci,id=ahci",
		"-device", "ide-hd,drive=build-format,id=disk0,bus=ahci.0",
		"-device", "virtio-balloon",
	}
	switch arch {
	case "amd64", "386":
		qemuCmd = append(qemuCmd,
			"--enable-kvm",
			"-cpu", "host",
			"-smp", strconv.Itoa(runtime.NumCPU()),
			"-m", "16384,slots=2,maxmem=32G",
		)
	case "arm64":
		qemuCmd = append(qemuCmd,
			"-cpu", "max", //"cortex-a53",
			"-smp", "4",
			//"-serial", "stdio",
			//"-append", "console=ttyS0",
			"-m", "4096,slots=2,maxmem=16G",
		)
	}

	log.Printf("Running QEMU: %s", strings.Join(qemuCmd, " "))
	c := exec.Command(qemuCmd[0], qemuCmd[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("failed to start QEMU: %w", err)
	}

	// let's try to connect to this port
	log.Printf("Waiting for qemu to finish loading...")

	const maxRetries = 60 // 2 minutes max wait time
	for i := 0; i < maxRetries; i++ {
		sshc, err = ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
		if err != nil {
			// Check if QEMU process is still running
			if c.ProcessState != nil && c.ProcessState.Exited() {
				return nil, fmt.Errorf("QEMU process exited unexpectedly")
			}
			time.Sleep(2 * time.Second)
			continue
		}
		be, err := NewSshBackend(sshc)
		if err != nil {
			sshc.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		return be, nil
	}

	// Timeout reached, kill QEMU
	c.Process.Kill()
	return nil, fmt.Errorf("timeout waiting for QEMU to become ready after %d seconds", maxRetries*2)
}

func (e *buildEnv) initQemu() error {
	be, err := NewQemuBackend(e.os, e.arch)
	if err != nil {
		return err
	}
	e.backend = be
	return nil
}

const initData = `#!/usr/azusa/busybox ash

mkdir /bin /sbin
/usr/azusa/busybox --install

mkdir /proc
mount -t proc proc /proc

mkdir -p /etc
ln -snf /proc/self/mounts /etc/mtab

mkdir -p /sys /tmp /var/log

mount -t sysfs sys /sys

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
mkdir -p /dev/pts /dev/shm
mount -t devpts devpts /dev/pts
mount -t tmpfs tmpfs /dev/shm

find /sys -name modalias -print0 | xargs -0 sort -u | xargs /sbin/modprobe -a

ip link set lo up
ip link set eth0 up
udhcpc -n -i eth0 -s /usr/azusa/simple.script

mkdir -p /var/lib/apkg
mount -t tmpfs tmpfs /var/lib/apkg

modprobe fuse
/usr/azusa/apkg -load_unsigned >/var/log/apkg.log 2>&1 &

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

ln -snf /pkg/main/azusa.symlinks.core.linux.__ARCH__/etc/xml /etc/xml

dbus-uuidgen --ensure=/etc/machine-id

mkdir -p /build
if [ -e /dev/vda ]; then
	echo "Formatting build disk..."
	mkfs.ext4 /dev/vda
	mount -t ext4 /dev/vda /build
elif [ -e /dev/sda ]; then
	echo "Formatting build disk..."
	mkfs.ext4 /dev/sda
	mount -t ext4 /dev/sda /build
fi

# move apkg here (not nice but good enough for us)
mkdir /build/apkg
mv /var/lib/apkg/main /var/lib/apkg/main.old
ln -snf /build/apkg /var/lib/apkg/main

# disable ldconfig
mkdir /build/bin
echo '#!/bin/bash' >/build/bin/ldconfig
chmod +x /build/bin/ldconfig

# set root password to empty
sed -i 's/root:\*:/root::/' /etc/shadow

echo "Running dropbear..."
mkdir /etc/dropbear
dropbear -E -B -R

# Initialize activity timestamp
touch /var/run/last_activity

# Idle timeout watchdog (1 hour = 3600 seconds)
(
	IDLE_TIMEOUT=3600
	while true; do
		sleep 60
		# Check for active SSH sessions (dropbear children)
		if pgrep -P $(pgrep -x dropbear | head -1) >/dev/null 2>&1; then
			# Active session, update timestamp
			touch /var/run/last_activity
		else
			# No active session, check how long we've been idle
			if [ -f /var/run/last_activity ]; then
				last_activity=$(stat -c %Y /var/run/last_activity)
				now=$(date +%s)
				idle_time=$((now - last_activity))
				if [ $idle_time -ge $IDLE_TIMEOUT ]; then
					echo "Idle timeout reached ($idle_time seconds), shutting down..."
					poweroff -f
				fi
			fi
		fi
	done
) &

/bin/bash -i || ash -i
poweroff -f
`

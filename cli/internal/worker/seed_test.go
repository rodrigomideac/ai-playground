package worker

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

func TestBuildSeedISO_NoCloudShape(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "seed.iso")

	const (
		hostname = "smoke-001"
		user     = "vm"
		pubKey   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAA test@host"
	)

	if err := BuildSeedISO(context.Background(), out, hostname, user, pubKey, true); err != nil {
		t.Fatalf("BuildSeedISO: %v", err)
	}

	st, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat seed iso: %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("seed iso is empty")
	}

	d, err := diskfs.Open(out)
	if err != nil {
		t.Fatalf("reopen seed iso: %v", err)
	}
	fs, err := d.GetFilesystem(0)
	if err != nil {
		t.Fatalf("read filesystem: %v", err)
	}
	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		t.Fatalf("filesystem type: want *iso9660.FileSystem, got %T", fs)
	}
	// go-diskfs returns the raw 32-byte PVD field; the kernel/blkid trim
	// trailing padding when the label is exposed to userspace.
	if got := strings.TrimRight(iso.Label(), " \x00"); got != "CIDATA" {
		t.Fatalf("volume label: want %q, got %q (raw %q)", "CIDATA", got, iso.Label())
	}

	ud := readISOFile(t, fs, "/user-data")
	for _, want := range [][]byte{
		[]byte("hostname: " + hostname),
		[]byte("name: " + user),
		[]byte(pubKey),
		[]byte("9p"),
		[]byte("hostshare"),
	} {
		if !bytes.Contains(ud, want) {
			t.Errorf("user-data missing %q\n--- got ---\n%s", want, ud)
		}
	}

	md := readISOFile(t, fs, "/meta-data")
	for _, want := range [][]byte{
		[]byte("instance-id: " + hostname),
		[]byte("local-hostname: " + hostname),
	} {
		if !bytes.Contains(md, want) {
			t.Errorf("meta-data missing %q\n--- got ---\n%s", want, md)
		}
	}
}

func TestBuildSeedISO_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "seed.iso")
	if err := os.WriteFile(out, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := BuildSeedISO(context.Background(), out, "host", "vm", "ssh-ed25519 KEY", false); err != nil {
		t.Fatalf("BuildSeedISO over existing file: %v", err)
	}
}

// TestBuildSeedISO_BlkidLabel checks that blkid (the same probe the kernel
// uses to publish LABEL=… to udev/cloud-init) reports CIDATA. Skipped when
// blkid isn't on PATH so the test is portable.
func TestBuildSeedISO_BlkidLabel(t *testing.T) {
	blkid, err := exec.LookPath("blkid")
	if err != nil {
		t.Skip("blkid not available")
	}
	out := filepath.Join(t.TempDir(), "seed.iso")
	if err := BuildSeedISO(context.Background(), out, "host", "vm", "ssh-ed25519 KEY", false); err != nil {
		t.Fatalf("BuildSeedISO: %v", err)
	}
	b, err := exec.Command(blkid, "-o", "value", "-s", "LABEL", out).Output()
	if err != nil {
		t.Fatalf("blkid: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != "CIDATA" {
		t.Fatalf("blkid LABEL: want CIDATA, got %q", got)
	}
}

func TestBuildSeedISO_NoMount(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "seed.iso")
	if err := BuildSeedISO(context.Background(), out, "host", "vm", "ssh-ed25519 KEY", false); err != nil {
		t.Fatalf("BuildSeedISO: %v", err)
	}
	d, err := diskfs.Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	fs, err := d.GetFilesystem(0)
	if err != nil {
		t.Fatal(err)
	}
	ud := readISOFile(t, fs, "/user-data")
	for _, banned := range [][]byte{[]byte("9p"), []byte("hostshare"), []byte("mounts:")} {
		if bytes.Contains(ud, banned) {
			t.Errorf("user-data with hostMount=false should not contain %q\n%s", banned, ud)
		}
	}
}

func readISOFile(t *testing.T, fs filesystem.FileSystem, path string) []byte {
	t.Helper()
	f, err := fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

// userDataTpl is rendered per-VM to drive cloud-init's first boot.
// The 9p mount is conditional on HostMount being non-empty.
const userDataTpl = `#cloud-config
hostname: {{.Hostname}}
fqdn: {{.Hostname}}.local
users:
  - name: {{.User}}
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - {{.PubKey}}
ssh_pwauth: false
{{if .HostMount}}
bootcmd:
  - modprobe 9p || true
  - modprobe 9pnet_virtio || true
mounts:
  - [hostshare, /home/{{.User}}/project, 9p, "trans=virtio,version=9p2000.L,access=any", "0", "0"]
runcmd:
  - mkdir -p /home/{{.User}}/project
  - chown {{.User}}:{{.User}} /home/{{.User}}/project
{{end}}
`

type seedData struct {
	Hostname  string
	User      string
	PubKey    string
	HostMount bool
}

// BuildSeedISO writes a NoCloud seed ISO at out. The ISO contains user-data
// (creates the worker user with the given pubkey, optionally configures a
// virtio-9p mount) and meta-data (instance-id + hostname). The image is built
// in-process with go-diskfs — cloud-init's NoCloud datasource locates the seed
// by the CIDATA volume label, with Rock Ridge preserving the long filenames.
func BuildSeedISO(_ context.Context, out, hostname, user, pubKey string, hostMount bool) error {
	var ud strings.Builder
	tpl := template.Must(template.New("user-data").Parse(userDataTpl))
	if err := tpl.Execute(&ud, seedData{
		Hostname:  hostname,
		User:      user,
		PubKey:    pubKey,
		HostMount: hostMount,
	}); err != nil {
		return fmt.Errorf("render user-data: %w", err)
	}
	md := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", hostname, hostname)

	// diskfs.Create refuses to write into an existing path.
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear seed ISO destination %s: %w", out, err)
	}

	const isoSize = 10 * 1024 * 1024
	d, err := diskfs.Create(out, isoSize, diskfs.SectorSizeDefault)
	if err != nil {
		return fmt.Errorf("create seed ISO image %s: %w", out, err)
	}
	d.LogicalBlocksize = 2048

	fs, err := d.CreateFilesystem(disk.FilesystemSpec{
		Partition: 0,
		FSType:    filesystem.TypeISO9660,
	})
	if err != nil {
		return fmt.Errorf("create ISO9660 filesystem: %w", err)
	}

	if err := writeSeedFile(fs, "user-data", []byte(ud.String())); err != nil {
		return err
	}
	if err := writeSeedFile(fs, "meta-data", []byte(md)); err != nil {
		return err
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return fmt.Errorf("seed filesystem is not iso9660 (got %T)", fs)
	}
	// VolumeIdentifier here is what lands in the PVD — go-diskfs ignores the
	// VolumeLabel passed to FilesystemSpec for ISO9660. cloud-init's NoCloud
	// probe matches on this exact string.
	if err := iso.Finalize(iso9660.FinalizeOptions{
		RockRidge:        true,
		VolumeIdentifier: "CIDATA",
	}); err != nil {
		return fmt.Errorf("finalize seed ISO: %w", err)
	}
	return nil
}

func writeSeedFile(fs filesystem.FileSystem, name string, content []byte) error {
	f, err := fs.OpenFile(name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return fmt.Errorf("create %s in seed ISO: %w", name, err)
	}
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("write %s into seed ISO: %w", name, err)
	}
	return nil
}

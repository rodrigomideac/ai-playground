package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
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
// virtio-9p mount) and meta-data (instance-id + hostname).
func BuildSeedISO(ctx context.Context, out, hostname, user, pubKey string, hostMount bool) error {
	dir, err := os.MkdirTemp("", "ai-playground-seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

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
	if err := os.WriteFile(filepath.Join(dir, "user-data"), []byte(ud.String()), 0o644); err != nil {
		return err
	}

	md := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", hostname, hostname)
	if err := os.WriteFile(filepath.Join(dir, "meta-data"), []byte(md), 0o644); err != nil {
		return err
	}

	return run(ctx, "xorriso", "-as", "mkisofs", "-quiet",
		"-volid", "CIDATA", "-joliet", "-rock",
		"-output", out,
		filepath.Join(dir, "user-data"),
		filepath.Join(dir, "meta-data"))
}

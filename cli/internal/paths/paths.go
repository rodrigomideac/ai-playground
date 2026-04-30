// Package paths resolves the XDG-compliant filesystem layout used by
// `ai-playground` for config, cache, and data, plus the helper for
// distro-family detection that gates command entry.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LibvirtPoolDir is the libvirt 'default' storage pool path. ai-playground
// writes the golden image here, and per-worker qcow2 overlays + NoCloud
// seed ISOs are also created here. The directory is set up by the doctor's
// host-prep flow as 'libvirt:libvirt 2770' so members of the libvirt group
// can write files into it that inherit group=libvirt (setgid). Storing the
// golden image inside the libvirt pool is what lets libvirt-qemu (the
// runtime user under libvirt-daemon-system on Debian/Ubuntu) read the
// backing file directly — without traversing the user's $HOME, which is
// 0700 by default and otherwise blocks the open.
const LibvirtPoolDir = "/var/lib/libvirt/images"

// GoldenImageName is the filename of the golden qcow2 within the libvirt
// pool. Per-worker overlay disks share the same directory but use the
// 'aip-<worker>.qcow2' / 'aip-<worker>-seed.iso' naming from manager.go.
const GoldenImageName = "ai-playground-base.qcow2"

// Paths is the resolved set of filesystem locations the CLI uses.
type Paths struct {
	// Roots
	ConfigDir string // $XDG_CONFIG_HOME/ai-playground
	CacheDir  string // $XDG_CACHE_HOME/ai-playground
	DataDir   string // $XDG_DATA_HOME/ai-playground

	// Under ConfigDir
	Config       string // config.yaml
	BuildDir     string // build/
	Template     string // build/template.pkr.hcl
	RunProvision string // build/run-provision.sh
	ProvisionDir string // build/provision/
	ChrootDir    string // build/chroot/

	// Under CacheDir
	RepoCache string // repo/   (clone of public repo)
	SeedDir   string // seed/   (build-only keypair + rendered user-data)
	PackerDir string // packer/ (Packer working dir / artifact_dir)

	// Under DataDir
	BinDir string // bin/    (vendored CLI tools, e.g. packer)

	// In the libvirt pool (system-level path; not under DataDir so that
	// libvirt-qemu can read it without crossing the user's home directory).
	GoldenImage string // /var/lib/libvirt/images/ai-playground-base.qcow2
}

// Default builds Paths from the XDG environment with the standard fallbacks.
func Default() (*Paths, error) {
	cfgHome, err := xdg("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, err
	}
	cacheHome, err := xdg("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return nil, err
	}
	dataHome, err := xdg("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return nil, err
	}
	return New(cfgHome, cacheHome, dataHome), nil
}

// New builds Paths from explicit XDG roots.
func New(configHome, cacheHome, dataHome string) *Paths {
	cfgDir := filepath.Join(configHome, "ai-playground")
	cacheDir := filepath.Join(cacheHome, "ai-playground")
	dataDir := filepath.Join(dataHome, "ai-playground")
	buildDir := filepath.Join(cfgDir, "build")
	return &Paths{
		ConfigDir:    cfgDir,
		CacheDir:     cacheDir,
		DataDir:      dataDir,
		Config:       filepath.Join(cfgDir, "config.yaml"),
		BuildDir:     buildDir,
		Template:     filepath.Join(buildDir, "template.pkr.hcl"),
		RunProvision: filepath.Join(buildDir, "run-provision.sh"),
		ProvisionDir: filepath.Join(buildDir, "provision"),
		ChrootDir:    filepath.Join(buildDir, "chroot"),
		RepoCache:    filepath.Join(cacheDir, "repo"),
		SeedDir:      filepath.Join(cacheDir, "seed"),
		PackerDir:    filepath.Join(cacheDir, "packer"),
		BinDir:       filepath.Join(dataDir, "bin"),
		GoldenImage:  filepath.Join(LibvirtPoolDir, GoldenImageName),
	}
}

// BuildPopulated reports whether the build/ dir has at least the template,
// runner, and a non-empty provision dir. Used by the state machine to
// distinguish "fresh setup" from "already initialized".
func (p *Paths) BuildPopulated() bool {
	if !fileExists(p.Template) || !fileExists(p.RunProvision) {
		return false
	}
	entries, err := os.ReadDir(p.ProvisionDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
			return true
		}
	}
	return false
}

// ConfigExists reports whether config.yaml is on disk.
func (p *Paths) ConfigExists() bool { return fileExists(p.Config) }

// GoldenExists reports whether the golden qcow2 has been built.
func (p *Paths) GoldenExists() bool { return fileExists(p.GoldenImage) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func xdg(envVar, fallback string) (string, error) {
	if v := os.Getenv(envVar); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for %s fallback: %w", envVar, err)
	}
	return filepath.Join(home, fallback), nil
}

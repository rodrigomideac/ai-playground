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
	GoldenDir   string // golden/
	GoldenImage string // golden/ai-playground-base.qcow2
	BinDir      string // bin/    (vendored CLI tools, e.g. packer)
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
	goldenDir := filepath.Join(dataDir, "golden")
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
		GoldenDir:    goldenDir,
		GoldenImage:  filepath.Join(goldenDir, "ai-playground-base.qcow2"),
		BinDir:       filepath.Join(dataDir, "bin"),
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

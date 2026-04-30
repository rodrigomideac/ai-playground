// Package repo resolves the source tree from which `init` and `build`
// populate the user's config dir — either a local override or a clone
// of the public repo cached under $XDG_CACHE_HOME.
package repo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
)

// CloneURL is the upstream repository the cache mirrors.
const CloneURL = "https://github.com/rodrigomideac/ai-playground"

// Source describes where the CLI is reading the repo files from.
type Source struct {
	Path       string // root of the repo tree
	IsOverride bool   // true when --repo-path / AI_PLAYGROUND_REPO is in effect
}

// PackerDir returns the absolute path to packer/ inside the source.
func (s *Source) PackerDir() string { return filepath.Join(s.Path, "packer") }

// ProvisionDir returns packer/provision/.
func (s *Source) ProvisionDir() string { return filepath.Join(s.PackerDir(), "provision") }

// Template returns packer/template.pkr.hcl.
func (s *Source) Template() string { return filepath.Join(s.PackerDir(), "template.pkr.hcl") }

// RunProvision returns packer/run-provision.sh.
func (s *Source) RunProvision() string { return filepath.Join(s.PackerDir(), "run-provision.sh") }

// SeedTemplate returns packer/seed/user-data.tpl.
func (s *Source) SeedTemplate() string { return filepath.Join(s.PackerDir(), "seed", "user-data.tpl") }

// SeedMeta returns packer/seed/meta-data.
func (s *Source) SeedMeta() string { return filepath.Join(s.PackerDir(), "seed", "meta-data") }

// Chroot returns chroot/.
func (s *Source) Chroot() string { return filepath.Join(s.Path, "chroot") }

// Resolve returns a Source ready for use. It prefers an override path
// (validated, never written to). Otherwise it ensures the cache exists
// and is fast-forwarded to origin/master.
func Resolve(ctx context.Context, override, cachePath string) (*Source, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return nil, fmt.Errorf("resolve override path: %w", err)
		}
		s := &Source{Path: abs, IsOverride: true}
		if err := validateLayout(s); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err := ensureCache(ctx, cachePath); err != nil {
		return nil, err
	}
	return &Source{Path: cachePath, IsOverride: false}, nil
}

func validateLayout(s *Source) error {
	required := []string{
		s.Template(),
		s.ProvisionDir(),
		s.RunProvision(),
		s.SeedTemplate(),
		s.SeedMeta(),
		s.Chroot(),
	}
	for _, p := range required {
		if _, err := os.Stat(p); err != nil {
			rel := strings.TrimPrefix(p, s.Path+string(filepath.Separator))
			return fmt.Errorf("repo source %s missing %s", s.Path, rel)
		}
	}
	return nil
}

func ensureCache(ctx context.Context, cachePath string) error {
	gitDir := filepath.Join(cachePath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		ui.Detail("Updating cache at %s (git fetch + reset --hard origin/master)", cachePath)
		if err := runStreamed(ctx, "git", "-C", cachePath, "fetch", "--quiet", "origin", "master"); err != nil {
			return fmt.Errorf("git fetch in %s: %w", cachePath, err)
		}
		if err := runStreamed(ctx, "git", "-C", cachePath, "reset", "--quiet", "--hard", "origin/master"); err != nil {
			return fmt.Errorf("git reset in %s: %w", cachePath, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("create parent dir for cache: %w", err)
	}
	ui.Detail("Cloning %s → %s", CloneURL, cachePath)
	if err := runStreamed(ctx, "git", "clone", "--quiet", CloneURL, cachePath); err != nil {
		return fmt.Errorf("git clone %s: %w", CloneURL, err)
	}
	return nil
}

func runStreamed(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Package buildflow contains the shared logic between `init` and `build`
// for populating $XDG_CONFIG_HOME/ai-playground/build/ from the repo
// source and detecting drift between user state and upstream defaults.
package buildflow

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rodrigomideac/ai-playground/cli/internal/config"
	"github.com/rodrigomideac/ai-playground/cli/internal/paths"
	"github.com/rodrigomideac/ai-playground/cli/internal/repo"
	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
)

// Populate copies the template, runner, chroot/, and the selected provision
// scripts from the repo source into build/. Honors cfg.OnConflict for files
// that already exist in build/ but differ from the source.
//
// `accepted` is the explicit list of script filenames to copy. When called
// from init's handholding, it's the user's per-script approvals; from build
// or headless init, it's cfg.Provision.Include.
func Populate(p *paths.Paths, src *repo.Source, cfg *config.Config, accepted []string) error {
	if err := os.MkdirAll(p.BuildDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", p.BuildDir, err)
	}
	if err := os.MkdirAll(p.ProvisionDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", p.ProvisionDir, err)
	}

	policy := cfg.Effective()

	if err := copyFile(src.Template(), p.Template, policy); err != nil {
		return fmt.Errorf("copy template: %w", err)
	}
	if err := copyFile(src.RunProvision(), p.RunProvision, policy); err != nil {
		return fmt.Errorf("copy run-provision.sh: %w", err)
	}
	if err := os.Chmod(p.RunProvision, 0o755); err != nil {
		return fmt.Errorf("chmod runner: %w", err)
	}
	ui.Detail("Copied template.pkr.hcl + run-provision.sh")

	for _, name := range accepted {
		srcPath := filepath.Join(src.ProvisionDir(), name)
		if _, err := os.Stat(srcPath); err != nil {
			return fmt.Errorf("provision.include references unknown script: %s", name)
		}
		dst := filepath.Join(p.ProvisionDir, name)
		if err := copyFile(srcPath, dst, policy); err != nil {
			return fmt.Errorf("copy provision %s: %w", name, err)
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", dst, err)
		}
	}
	ui.Detail("Copied %d provision script(s) into provision/", len(accepted))

	if err := copyTree(src.Chroot(), p.ChrootDir, policy); err != nil {
		return fmt.Errorf("copy chroot/: %w", err)
	}
	ui.Detail("Copied chroot/ overlay")

	if err := PopulateSeedTemplates(p, src); err != nil {
		return err
	}
	ui.Detail("Refreshed seed templates in %s", p.SeedDir)
	return nil
}

// PopulateSeedTemplates copies user-data.tpl and meta-data from the repo
// source into the seed cache. The build-only keypair and the rendered
// user-data are produced separately at build time.
func PopulateSeedTemplates(p *paths.Paths, src *repo.Source) error {
	if err := os.MkdirAll(p.SeedDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", p.SeedDir, err)
	}
	for _, pair := range []struct{ src, dst string }{
		{src.SeedTemplate(), filepath.Join(p.SeedDir, "user-data.tpl")},
		{src.SeedMeta(), filepath.Join(p.SeedDir, "meta-data")},
	} {
		// Always overwrite — these aren't user-edited.
		data, err := os.ReadFile(pair.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", pair.src, err)
		}
		if err := os.WriteFile(pair.dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", pair.dst, err)
		}
	}
	return nil
}

// copyFile copies src to dst with the given on-conflict policy.
func copyFile(src, dst, onConflict string) error {
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(dst); err == nil {
		if sha256sum(srcBytes) == sha256sum(existing) {
			return nil
		}
		if onConflict == config.OnConflictKeep {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, srcBytes, 0o644)
}

// copyTree mirrors src into dst, applying onConflict per-file.
func copyTree(src, dst, onConflict string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("expected directory: %s", src)
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		// Symlinks: preserve as symlinks.
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if existing, err := os.Readlink(target); err == nil {
				if existing == link {
					return nil
				}
				if onConflict == config.OnConflictKeep {
					return nil
				}
				_ = os.Remove(target)
			}
			return os.Symlink(link, target)
		}
		return copyFile(path, target, onConflict)
	})
}

func sha256sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ScriptDescription extracts the `# Description: ...` header from a shell
// script. Empty string when the header is absent.
func ScriptDescription(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 64*1024))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "# Description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# Description:")), nil
		}
	}
	return "", sc.Err()
}

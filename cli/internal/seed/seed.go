// Package seed prepares the build-only NoCloud seed used by Packer's qemu
// builder: a throwaway ed25519 keypair and the rendered user-data file.
// Replaces scripts/prepare-packer-seed.sh.
package seed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rodrigomideac/ai-playground/internal/ui"
)

// EnsureKeypair creates id_ed25519/id_ed25519.pub under seedDir if absent.
// Idempotent: re-running with an existing keypair is a no-op.
func EnsureKeypair(ctx context.Context, seedDir string) error {
	if err := os.MkdirAll(seedDir, 0o700); err != nil {
		return fmt.Errorf("create seed dir %s: %w", seedDir, err)
	}
	keyPath := filepath.Join(seedDir, "id_ed25519")
	if _, err := os.Stat(keyPath); err == nil {
		ui.Detail("Build keypair already present at %s", keyPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", keyPath, err)
	}
	ui.Detail("Generating build SSH key at %s", keyPath)
	cmd := exec.CommandContext(ctx, "ssh-keygen",
		"-t", "ed25519",
		"-N", "",
		"-C", "packer-build@ai-playground",
		"-f", keyPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-keygen: %w", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", keyPath, err)
	}
	return nil
}

// RenderUserData substitutes __SSH_PUBKEY__ in templatePath with the
// public key from seedDir/id_ed25519.pub and writes seedDir/user-data.
func RenderUserData(seedDir, templatePath string) error {
	pub, err := os.ReadFile(filepath.Join(seedDir, "id_ed25519.pub"))
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	tpl, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", templatePath, err)
	}
	rendered := strings.ReplaceAll(string(tpl), "__SSH_PUBKEY__", strings.TrimSpace(string(pub)))
	out := filepath.Join(seedDir, "user-data")
	if err := os.WriteFile(out, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	ui.Detail("Rendered %s", out)
	return nil
}

// CopyMetaData copies a meta-data file from src into seedDir.
func CopyMetaData(src, seedDir string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	out := filepath.Join(seedDir, "meta-data")
	return os.WriteFile(out, data, 0o644)
}

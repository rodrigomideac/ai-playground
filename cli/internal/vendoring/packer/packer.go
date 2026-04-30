// Package packer downloads and installs a pinned HashiCorp Packer
// release into the user's data directory, so ai-playground does not
// require Packer to be on PATH on the host. The vendored binary is
// integrity-checked against a SHA256 baked into the CLI; bumping the
// pin means updating Version + Checksums in this file together.
package packer

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version is the pinned Packer release. Updating this constant requires
// updating Checksums in lockstep — Ensure refuses to install a binary
// whose SHA256 does not match the entry for runtime.GOOS_runtime.GOARCH.
const Version = "1.15.3"

// Checksums maps "<goos>_<goarch>" to the SHA256 of the corresponding
// packer_<Version>_<goos>_<goarch>.zip from releases.hashicorp.com.
// Sourced from the official packer_<Version>_SHA256SUMS file.
var Checksums = map[string]string{
	"linux_amd64":  "9ed712c9a8f223c7985d7d21c6b65744bf1c66b8aca333232b96f5ae3fd9c90d",
	"linux_arm64":  "ebf06f8f30a7e3bc69fa33ac8a5dfffefd70187df4541cccc6ef3f325b8ae4f1",
	"darwin_amd64": "7718bee4a580d7c486263f10a28d00db7e8c600af08102ae118592ee30a50892",
	"darwin_arm64": "cca1601f2d187b084aa875183ae70e85521df0475fc0f61a7380e46df8980289",
}

// DefaultBaseURL is the prefix used to build download URLs. Tests
// override this via Vendor.BaseURL.
const DefaultBaseURL = "https://releases.hashicorp.com"

// Vendor configures a single packer-install operation. Production
// callers use Default; tests construct a Vendor pointing at an
// httptest.Server with custom Checksums.
type Vendor struct {
	Version   string
	BinDir    string
	BaseURL   string
	Checksums map[string]string
	Client    *http.Client
	// OS and Arch override runtime.GOOS / runtime.GOARCH for tests.
	OS, Arch string
}

// Default returns a Vendor configured against the official HashiCorp
// release server with the pinned Version and Checksums above.
func Default(binDir string) *Vendor {
	return &Vendor{
		Version:   Version,
		BinDir:    binDir,
		BaseURL:   DefaultBaseURL,
		Checksums: Checksums,
		Client:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// BinaryPath returns the absolute path where the vendored binary lives,
// regardless of whether it is currently installed.
func (v *Vendor) BinaryPath() string {
	return filepath.Join(v.BinDir, "packer-"+v.Version)
}

// Ensure returns the absolute path of a verified packer binary at the
// configured Version, downloading and installing it under BinDir if
// it is not already there. Successful installs garbage-collect older
// `packer-*` binaries in the same directory.
func (v *Vendor) Ensure(ctx context.Context) (string, error) {
	target := v.BinaryPath()
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		return target, nil
	}

	key := v.osArchKey()
	want, ok := v.Checksums[key]
	if !ok {
		return "", fmt.Errorf("vendored Packer is not built for %s; ai-playground only supports %s", key, sortedKeys(v.Checksums))
	}

	if err := os.MkdirAll(v.BinDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin directory %s: %w", v.BinDir, err)
	}

	zipName := fmt.Sprintf("packer_%s_%s.zip", v.Version, key)
	url := strings.TrimRight(v.BaseURL, "/") + "/packer/" + v.Version + "/" + zipName

	tmpZip, err := os.CreateTemp(v.BinDir, "packer-*.zip.part")
	if err != nil {
		return "", fmt.Errorf("create temp file for packer download: %w", err)
	}
	tmpPath := tmpZip.Name()
	defer func() {
		_ = tmpZip.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := downloadVerify(ctx, v.client(), url, tmpZip, want); err != nil {
		return "", err
	}
	if err := tmpZip.Close(); err != nil {
		return "", fmt.Errorf("close packer download: %w", err)
	}

	if err := extractPackerBinary(tmpPath, target, v.os()); err != nil {
		return "", err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return "", fmt.Errorf("chmod packer binary: %w", err)
	}

	v.gcOldVersions(target)
	return target, nil
}

func (v *Vendor) client() *http.Client {
	if v.Client != nil {
		return v.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (v *Vendor) os() string {
	if v.OS != "" {
		return v.OS
	}
	return runtime.GOOS
}

func (v *Vendor) arch() string {
	if v.Arch != "" {
		return v.Arch
	}
	return runtime.GOARCH
}

func (v *Vendor) osArchKey() string { return v.os() + "_" + v.arch() }

// gcOldVersions removes any `packer-<other>` files in BinDir. Best-effort:
// failures are ignored so a stale binary doesn't block a successful install.
func (v *Vendor) gcOldVersions(keep string) {
	entries, err := os.ReadDir(v.BinDir)
	if err != nil {
		return
	}
	keepName := filepath.Base(keep)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == keepName || !strings.HasPrefix(name, "packer-") {
			continue
		}
		if strings.HasSuffix(name, ".part") {
			continue
		}
		_ = os.Remove(filepath.Join(v.BinDir, name))
	}
}

func downloadVerify(ctx context.Context, client *http.Client, url string, w io.Writer, wantHex string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build packer download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download packer from %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download packer from %s: HTTP %d", url, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), resp.Body); err != nil {
		return fmt.Errorf("read packer download: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantHex {
		return fmt.Errorf("packer SHA256 mismatch for %s: want %s, got %s", url, wantHex, got)
	}
	return nil
}

// extractPackerBinary unzips the single `packer` (or `packer.exe`) entry
// out of zipPath into outPath. The HashiCorp release zips contain
// exactly that one file at the root.
func extractPackerBinary(zipPath, outPath, goos string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open packer zip: %w", err)
	}
	defer r.Close()
	wantName := "packer"
	if goos == "windows" {
		wantName = "packer.exe"
	}
	for _, f := range r.File {
		if f.Name != wantName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s inside zip: %w", wantName, err)
		}
		defer rc.Close()
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = os.Remove(outPath)
			return fmt.Errorf("write packer binary: %w", err)
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(outPath)
			return fmt.Errorf("close packer binary: %w", err)
		}
		return nil
	}
	return fmt.Errorf("packer zip did not contain %s", wantName)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// stdlib sort would pull in another import; bubble is fine for ≤8 keys.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

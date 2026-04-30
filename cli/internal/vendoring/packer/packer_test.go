package packer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makePackerZip(t *testing.T, body []byte, entry string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fakeReleases serves a single packer zip at the same path layout as
// releases.hashicorp.com. Returns the test server and the SHA256 of
// the served zip.
func fakeReleases(t *testing.T, version, osArch, entry string, body []byte) (*httptest.Server, string) {
	t.Helper()
	zipBytes := makePackerZip(t, body, entry)
	wantPath := "/packer/" + version + "/packer_" + version + "_" + osArch + ".zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	}))
	t.Cleanup(srv.Close)
	return srv, sha256Hex(zipBytes)
}

func TestEnsure_HappyPath(t *testing.T) {
	const ver = "1.99.0"
	const osArch = "linux_amd64"
	body := []byte("#!/bin/sh\necho fake packer\n")
	srv, sum := fakeReleases(t, ver, osArch, "packer", body)

	v := &Vendor{
		Version:   ver,
		BinDir:    t.TempDir(),
		BaseURL:   srv.URL,
		Checksums: map[string]string{osArch: sum},
		OS:        "linux", Arch: "amd64",
	}

	got, err := v.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := filepath.Join(v.BinDir, "packer-"+ver)
	if got != want {
		t.Fatalf("path: want %q, got %q", want, got)
	}

	fi, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary not executable: mode=%v", fi.Mode())
	}
	gotBody, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("extracted body mismatch")
	}
}

func TestEnsure_Idempotent(t *testing.T) {
	const ver = "1.99.0"
	const osArch = "linux_amd64"
	body := []byte("fake")
	calls := 0
	zipBytes := makePackerZip(t, body, "packer")
	wantPath := "/packer/" + ver + "/packer_" + ver + "_" + osArch + ".zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		calls++
		_, _ = w.Write(zipBytes)
	}))
	t.Cleanup(srv.Close)

	v := &Vendor{
		Version: ver, BinDir: t.TempDir(), BaseURL: srv.URL,
		Checksums: map[string]string{osArch: sha256Hex(zipBytes)},
		OS:        "linux", Arch: "amd64",
	}
	if _, err := v.Ensure(context.Background()); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := v.Ensure(context.Background()); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 download, got %d", calls)
	}
}

func TestEnsure_ChecksumMismatch(t *testing.T) {
	const ver = "1.99.0"
	const osArch = "linux_amd64"
	srv, _ := fakeReleases(t, ver, osArch, "packer", []byte("fake"))

	v := &Vendor{
		Version: ver, BinDir: t.TempDir(), BaseURL: srv.URL,
		Checksums: map[string]string{osArch: strings.Repeat("0", 64)},
		OS:        "linux", Arch: "amd64",
	}
	_, err := v.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("error should mention SHA256 mismatch: %v", err)
	}
	if _, statErr := os.Stat(v.BinaryPath()); statErr == nil {
		t.Error("binary should not be installed after checksum failure")
	}
}

func TestEnsure_UnsupportedArch(t *testing.T) {
	v := &Vendor{
		Version:   "1.99.0",
		BinDir:    t.TempDir(),
		BaseURL:   "http://unused",
		Checksums: map[string]string{"linux_amd64": strings.Repeat("a", 64)},
		OS:        "plan9", Arch: "loong64",
	}
	_, err := v.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected unsupported-arch error")
	}
	if !strings.Contains(err.Error(), "plan9_loong64") {
		t.Errorf("error should mention requested arch: %v", err)
	}
}

func TestEnsure_GCsOldVersions(t *testing.T) {
	const ver = "2.0.0"
	const osArch = "linux_amd64"
	srv, sum := fakeReleases(t, ver, osArch, "packer", []byte("new"))

	bin := t.TempDir()
	for _, stale := range []string{"packer-1.0.0", "packer-1.50.0"} {
		if err := os.WriteFile(filepath.Join(bin, stale), []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// non-packer files must survive.
	if err := os.WriteFile(filepath.Join(bin, "other-tool"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	v := &Vendor{
		Version: ver, BinDir: bin, BaseURL: srv.URL,
		Checksums: map[string]string{osArch: sum},
		OS:        "linux", Arch: "amd64",
	}
	if _, err := v.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, stale := range []string{"packer-1.0.0", "packer-1.50.0"} {
		if _, err := os.Stat(filepath.Join(bin, stale)); err == nil {
			t.Errorf("stale %s should have been removed", stale)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "other-tool")); err != nil {
		t.Errorf("non-packer file other-tool should survive GC: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bin, "packer-"+ver)); err != nil {
		t.Errorf("new binary should exist: %v", err)
	}
}

func TestEnsure_ZipMissingBinary(t *testing.T) {
	const ver = "1.99.0"
	const osArch = "linux_amd64"
	// Zip contains a different filename — the extractor should error.
	srv, sum := fakeReleases(t, ver, osArch, "not-packer", []byte("x"))
	v := &Vendor{
		Version: ver, BinDir: t.TempDir(), BaseURL: srv.URL,
		Checksums: map[string]string{osArch: sum},
		OS:        "linux", Arch: "amd64",
	}
	_, err := v.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected error when zip lacks packer binary")
	}
	if !strings.Contains(err.Error(), "did not contain packer") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnsure_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	v := &Vendor{
		Version: "1.99.0", BinDir: t.TempDir(), BaseURL: srv.URL,
		Checksums: map[string]string{"linux_amd64": strings.Repeat("a", 64)},
		OS:        "linux", Arch: "amd64",
	}
	_, err := v.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProductionChecksums_AllKnownArchPresent(t *testing.T) {
	// Smoke-check the production map. If we add a checksum for a new arch,
	// this test (and Default) should still work.
	for _, k := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"} {
		if _, ok := Checksums[k]; !ok {
			t.Errorf("production Checksums missing entry for %s", k)
		}
	}
	for k, v := range Checksums {
		if len(v) != 64 {
			t.Errorf("%s checksum is not 64 hex chars: %q", k, v)
		}
	}
}

func TestDefault_ReturnsVendorWithProductionConfig(t *testing.T) {
	v := Default("/tmp/bin")
	if v.Version != Version {
		t.Errorf("Default Version: want %q, got %q", Version, v.Version)
	}
	if v.BaseURL != DefaultBaseURL {
		t.Errorf("Default BaseURL: want %q, got %q", DefaultBaseURL, v.BaseURL)
	}
	if v.BinDir != "/tmp/bin" {
		t.Errorf("BinDir: want %q, got %q", "/tmp/bin", v.BinDir)
	}
	if v.Client == nil {
		t.Error("Default Client should not be nil")
	}
	want := fmt.Sprintf("/tmp/bin/packer-%s", Version)
	if v.BinaryPath() != want {
		t.Errorf("BinaryPath: want %q, got %q", want, v.BinaryPath())
	}
}

package worker

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Worker is one libvirt domain spawned from the golden image — a single
// member of the worker pool.
type Worker struct {
	Name       string // user-facing, no prefix
	DomainName string // libvirt domain name (Prefix + "-" + Name)
	DiskPath   string // qcow2 overlay file
	SeedPath   string // NoCloud seed ISO
	State      string // libvirt state ("running", "shut off", ...) — populated by List/Get
}

// IP returns the first IPv4 lease on the libvirt network for this worker.
// Returns an error (not an empty string) when no lease is yet available, so
// callers can poll cleanly.
func (w *Worker) IP(ctx context.Context) (string, error) {
	out, err := runCapture(ctx, "virsh", "-c", "qemu:///system",
		"domifaddr", w.DomainName)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[2] == "ipv4" {
			ip := fields[3]
			if i := strings.Index(ip, "/"); i >= 0 {
				ip = ip[:i]
			}
			return ip, nil
		}
	}
	return "", fmt.Errorf("no IPv4 lease yet for %s", w.DomainName)
}

// IPWait polls IP() until it succeeds or timeout elapses.
func (w *Worker) IPWait(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		ip, err := w.IP(ctx)
		if err == nil {
			return ip, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for DHCP lease: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Destroy powers off the worker (best-effort) and removes the libvirt domain
// along with its storage volumes. Used by `shutdown-worker`.
func (w *Worker) Destroy(ctx context.Context) error {
	_ = runQuiet(ctx, "virsh", "-c", "qemu:///system", "destroy", w.DomainName)
	if err := runMuted(ctx, "virsh", "-c", "qemu:///system",
		"undefine", "--remove-all-storage", w.DomainName); err != nil {
		return fmt.Errorf("undefine domain: %w", err)
	}
	return nil
}

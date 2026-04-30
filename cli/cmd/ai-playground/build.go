package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/cli/internal/buildflow"
	"github.com/rodrigomideac/ai-playground/cli/internal/config"
	"github.com/rodrigomideac/ai-playground/cli/internal/doctor"
	"github.com/rodrigomideac/ai-playground/cli/internal/paths"
	"github.com/rodrigomideac/ai-playground/cli/internal/promptio"
	"github.com/rodrigomideac/ai-playground/cli/internal/repo"
	"github.com/rodrigomideac/ai-playground/cli/internal/seed"
	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
	vendoredpacker "github.com/rodrigomideac/ai-playground/cli/internal/vendoring/packer"
	"github.com/rodrigomideac/ai-playground/cli/internal/worker"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Run first-run setup if needed and build the golden image",
	Long: `build is the unified entrypoint. It detects state and either runs
interactive setup before building, or reads existing config and proceeds
straight to Packer. Idempotent — re-running after editing a script under
build/provision/ rebuilds the golden image with the change.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := contextWithTimeout(cmd.Context(), 30*time.Minute)
		defer cancel()
		return runBuild(ctx, cmd.OutOrStdout())
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
}

func runBuild(ctx context.Context, out io.Writer) error {
	if err := requireSupportedDistro(out); err != nil {
		return err
	}

	p := cliCtx.Paths
	configPresent := p.ConfigExists()
	buildPresent := p.BuildPopulated()

	switch {
	case !configPresent && buildPresent:
		return fmt.Errorf("inconsistent state — run 'ai-playground reset'")
	case !configPresent && !buildPresent:
		// First run: handholding requires a TTY; otherwise refer to headless.
		if !promptio.IsTTY() {
			return fmt.Errorf("build requires either an interactive terminal for first-run setup or a pre-populated config.yaml. See https://github.com/rodrigomideac/ai-playground#headless")
		}
		if err := runHandholding(ctx, out); err != nil {
			return err
		}
		// runHandholding sets cliCtx.Cfg.
	case configPresent && !buildPresent:
		// Headless populate per cfg.
		if err := runHeadlessPopulate(ctx, out, cliCtx.Cfg); err != nil {
			return err
		}
	}

	return runPackerBuild(ctx, out)
}

func runPackerBuild(ctx context.Context, out io.Writer) error {
	p := cliCtx.Paths
	cfg := cliCtx.Cfg
	if cfg == nil {
		return fmt.Errorf("internal error: build invoked without loaded config")
	}

	doneVal := ui.Step("Validating config and provision scripts")
	if err := validateProvisionScripts(p, cfg); err != nil {
		return err
	}
	doneVal("vm_user=%s, %d provision script(s)", cfg.VMUser, len(cfg.Provision.Include))

	doneDoc := ui.Step("Running cheap doctor checks")
	if probs := doctor.Run(ctx, true); len(probs) > 0 {
		doctor.PrintProblems(out, probs)
		return fmt.Errorf("doctor checks failed")
	}
	doneDoc("All cheap host checks passed")

	if err := ensureNoConflictingWorkers(ctx, out); err != nil {
		return err
	}

	doneSrc := ui.Step("Checking for upstream changes")
	src, err := repo.Resolve(ctx, repoOverride(), p.RepoCache)
	if err != nil {
		ui.Warn("Skipping upstream-changes check: %v", err)
	} else {
		doneSrc(sourceLabel(src))
		if drift, err := buildflow.Detect(src, cfg); err == nil {
			printDriftNotice(out, drift)
		}
	}

	if src != nil {
		doneSeed := ui.Step("Refreshing first-boot config templates")
		if err := buildflow.PopulateSeedTemplates(p, src); err != nil {
			ui.Warn("Could not refresh first-boot config templates: %v", err)
		} else {
			doneSeed(p.SeedDir)
		}
	}

	doneKey := ui.Step("Preparing build credentials")
	if err := seed.EnsureKeypair(ctx, p.SeedDir); err != nil {
		return err
	}
	tplPath := filepath.Join(p.SeedDir, "user-data.tpl")
	if _, err := os.Stat(tplPath); err != nil {
		return fmt.Errorf("first-boot config template missing at %s — try 'ai-playground init'", tplPath)
	}
	if err := seed.RenderUserData(p.SeedDir, tplPath); err != nil {
		return err
	}
	doneKey("Build SSH key + first-boot config ready")

	if err := os.MkdirAll(p.PackerDir, 0o755); err != nil {
		return err
	}

	doneVendor := ui.Step("Installing Packer %s (one-time download)", vendoredpacker.Version)
	packerBin, err := vendoredpacker.Default(p.BinDir).Ensure(ctx)
	if err != nil {
		return fmt.Errorf("install vendored Packer: %w", err)
	}
	doneVendor(packerBin)

	doneInit := ui.Step("Preparing Packer")
	if err := runPacker(ctx, out, p, packerBin, "init", "template.pkr.hcl"); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}
	doneInit("")

	// Packer's qemu builder refuses to start when its output_directory
	// already exists — leftover from a previous successful build (we move
	// the produced qcow2 out of it but the parent directory remains).
	priorOutput := filepath.Join(p.PackerDir, "packer-ai-playground-base")
	_ = os.RemoveAll(priorOutput)

	doneBuild := ui.Step("Building golden image — boots Debian, runs setup scripts, cleans up (~5 min on first run)")
	if err := runPacker(ctx, out, p, packerBin, "build",
		"-var", "seed_dir="+p.SeedDir,
		"template.pkr.hcl"); err != nil {
		return fmt.Errorf("packer build: %w", err)
	}
	doneBuild("")

	doneMove := ui.Step("Installing golden image")
	if err := moveGolden(p); err != nil {
		return err
	}
	doneMove(p.GoldenImage)

	ui.Success("Golden image ready at %s", p.GoldenImage)
	return nil
}

// ensureNoConflictingWorkers refuses to rebuild while existing workers
// reference the current golden image as their backing file. Each worker's
// per-VM disk is a qcow2 overlay whose backing-file path resolves to the
// golden image; once Packer overwrites that file, every still-defined
// worker (running or shut off) becomes inconsistent — qemu would read
// new-golden bytes through old COW chains. Worker disks must be deleted
// before the rebuild.
//
// In an interactive shell we offer to stop them; in non-TTY contexts we
// refuse with a pointer to shutdown-worker. This check fires after the
// cheap doctor pass (so an unhealthy host fails fast before we touch
// worker state).
func ensureNoConflictingWorkers(ctx context.Context, out io.Writer) error {
	m := &worker.Manager{Prefix: globalOpts.prefix}
	workers, err := m.List(ctx)
	if err != nil {
		return fmt.Errorf("could not list existing workers: %w", err)
	}
	if len(workers) == 0 {
		return nil
	}

	fmt.Fprintf(out, "\n%s Found %s existing worker(s) with prefix %s:\n",
		ui.Yellow("!"),
		ui.Bold(fmt.Sprintf("%d", len(workers))),
		ui.Bold(globalOpts.prefix))
	for _, w := range workers {
		fmt.Fprintf(out, "    - %s %s\n", w.Name, ui.Dim(fmt.Sprintf("(state=%s)", w.State)))
	}
	fmt.Fprintf(out, "\nEach worker's disk is a copy-on-write overlay backed by the golden image.\n"+
		"Rebuilding will replace that golden image, leaving these worker disks pointing at\n"+
		"a different file than the one they were created from. To keep the pool consistent,\n"+
		"all workers must be stopped before rebuilding.\n\n")

	if !promptio.IsTTY() {
		return fmt.Errorf("refusing to build with %d existing worker(s); run 'ai-playground shutdown-worker' for each first, or run 'ai-playground build' interactively to stop them all", len(workers))
	}

	prompt := promptio.New()
	ok, err := prompt.YesNo("Stop all of them and delete their disks before building?", true)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("aborting build to keep existing workers consistent with the current golden image")
	}

	for _, w := range workers {
		done := ui.Step("Stopping %s", w.Name)
		if err := w.Destroy(ctx); err != nil {
			return fmt.Errorf("stop worker %s: %w", w.Name, err)
		}
		done("")
	}
	return nil
}

func validateProvisionScripts(p *paths.Paths, cfg *config.Config) error {
	for _, name := range cfg.Provision.Include {
		full := filepath.Join(p.ProvisionDir, name)
		if _, err := os.Stat(full); err != nil {
			return fmt.Errorf("provision.include lists %q but it is missing from %s", name, p.ProvisionDir)
		}
	}
	return nil
}

func runPacker(ctx context.Context, out io.Writer, p *paths.Paths, packerBin string, args ...string) error {
	cmd := exec.CommandContext(ctx, packerBin, args...)
	cmd.Dir = p.BuildDir
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "ARTIFACT_DIR="+p.PackerDir)
	return cmd.Run()
}

func moveGolden(p *paths.Paths) error {
	produced := filepath.Join(p.PackerDir, "packer-ai-playground-base", "ai-playground-base")
	if _, err := os.Stat(produced); err != nil {
		return fmt.Errorf("packer build did not produce expected artifact at %s: %w", produced, err)
	}
	// Target directory is the libvirt pool. We rely on the doctor's
	// host-prep step to have created it group-owned by 'libvirt' with
	// mode 2770 — don't MkdirAll it here, since failure to do so would be
	// papering over a real misconfiguration.
	if _, err := os.Stat(paths.LibvirtPoolDir); err != nil {
		return fmt.Errorf("libvirt pool directory %s missing: run 'ai-playground doctor' to set it up: %w", paths.LibvirtPoolDir, err)
	}
	if err := os.Rename(produced, p.GoldenImage); err != nil {
		// Cross-filesystem fallback (e.g. /var on a separate fs from $XDG_CACHE_HOME).
		if err := copyGolden(produced, p.GoldenImage); err != nil {
			return fmt.Errorf("move golden: %w", err)
		}
		_ = os.Remove(produced)
	}
	// Normalize ownership + mode so libvirt-qemu can read the backing file
	// regardless of which path (rename vs copy fallback) we took. Rename
	// preserves the source inode's group (= the building user's primary gid),
	// while a fresh create inside the setgid pool dir inherits group=libvirt
	// — explicit chgrp avoids that path-dependent inconsistency. 0644 makes
	// reading robust to whether libvirt-qemu happens to be a member of the
	// libvirt group in the host's libvirt configuration.
	if err := chgrpToLibvirt(p.GoldenImage); err != nil {
		return fmt.Errorf("chgrp golden image to libvirt: %w", err)
	}
	if err := os.Chmod(p.GoldenImage, 0o644); err != nil {
		return fmt.Errorf("chmod golden image: %w", err)
	}
	return nil
}

// chgrpToLibvirt sets the group of path to the 'libvirt' POSIX group. The
// current user is expected to be in that group (verified by the doctor's
// libvirt-group check), which is the precondition for chown(2) to allow a
// non-root caller to change the group ownership.
func chgrpToLibvirt(path string) error {
	g, err := user.LookupGroup("libvirt")
	if err != nil {
		return fmt.Errorf("lookup libvirt group: %w", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("parse libvirt gid %q: %w", g.Gid, err)
	}
	return os.Chown(path, -1, gid)
}

func copyGolden(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func printDriftNotice(_ io.Writer, d *buildflow.Drift) {
	if len(d.NewUpstream) > 0 {
		var lines []string
		for _, name := range d.NewUpstream {
			lines = append(lines, "- "+name)
		}
		lines = append(lines, "Add their filenames to provision.include in config.yaml to opt in.")
		ui.Notice(fmt.Sprintf("%d new setup script(s) available in the upstream repo", len(d.NewUpstream)), lines)
	}
	if len(d.RemovedUpstream) > 0 {
		var lines []string
		for _, name := range d.RemovedUpstream {
			lines = append(lines, "- "+name)
		}
		ui.Notice(
			fmt.Sprintf("%d setup script(s) in provision.include no longer exist upstream (your local copies still run)", len(d.RemovedUpstream)),
			lines,
		)
	}
}


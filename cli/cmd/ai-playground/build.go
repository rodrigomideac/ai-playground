package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/buildflow"
	"github.com/rodrigomideac/ai-playground/internal/config"
	"github.com/rodrigomideac/ai-playground/internal/doctor"
	"github.com/rodrigomideac/ai-playground/internal/paths"
	"github.com/rodrigomideac/ai-playground/internal/promptio"
	"github.com/rodrigomideac/ai-playground/internal/repo"
	"github.com/rodrigomideac/ai-playground/internal/seed"
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

	if err := validateProvisionScripts(p, cfg); err != nil {
		return err
	}

	if probs := doctor.Run(ctx, true); len(probs) > 0 {
		doctor.PrintProblems(out, probs)
		return fmt.Errorf("doctor checks failed")
	}

	src, err := repo.Resolve(ctx, repoOverride(), p.RepoCache)
	if err != nil {
		// Drift detection is best-effort — if the cache can't be refreshed
		// (e.g. offline), continue rather than blocking the build.
		fmt.Fprintf(out, "warning: skipping upstream-drift check (%v)\n", err)
	} else {
		if drift, err := buildflow.Detect(src, cfg); err == nil {
			printDriftNotice(out, drift)
		}
	}

	if err := buildflow.PopulateSeedTemplates(p, src); err != nil {
		// Best-effort — only fails if templates aren't readable.
		fmt.Fprintf(out, "warning: could not refresh seed templates: %v\n", err)
	}

	if err := seed.EnsureKeypair(ctx, p.SeedDir); err != nil {
		return err
	}
	tplPath := filepath.Join(p.SeedDir, "user-data.tpl")
	if _, err := os.Stat(tplPath); err != nil {
		return fmt.Errorf("seed template missing at %s — try 'ai-playground init'", tplPath)
	}
	if err := seed.RenderUserData(p.SeedDir, tplPath); err != nil {
		return err
	}

	if err := os.MkdirAll(p.PackerDir, 0o755); err != nil {
		return err
	}

	if err := runPacker(ctx, out, p, "init", "template.pkr.hcl"); err != nil {
		return fmt.Errorf("packer init: %w", err)
	}
	if err := runPacker(ctx, out, p, "build",
		"-var", "seed_dir="+p.SeedDir,
		"template.pkr.hcl"); err != nil {
		return fmt.Errorf("packer build: %w", err)
	}

	if err := moveGolden(p); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nGolden image ready: %s\n", p.GoldenImage)
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

func runPacker(ctx context.Context, out io.Writer, p *paths.Paths, args ...string) error {
	cmd := exec.CommandContext(ctx, "packer", args...)
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
	if err := os.MkdirAll(p.GoldenDir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(produced, p.GoldenImage); err != nil {
		// Cross-filesystem fallback.
		if err := copyGolden(produced, p.GoldenImage); err != nil {
			return fmt.Errorf("move golden: %w", err)
		}
		_ = os.Remove(produced)
	}
	return nil
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

func printDriftNotice(out io.Writer, d *buildflow.Drift) {
	if len(d.NewUpstream) > 0 {
		fmt.Fprintf(out, "%d new default-provision script(s) are available upstream:\n", len(d.NewUpstream))
		for _, name := range d.NewUpstream {
			fmt.Fprintf(out, "  - %s\n", name)
		}
		fmt.Fprintln(out, "Add their filenames to provision.include in config.yaml to opt in.")
		fmt.Fprintln(out)
	}
	if len(d.RemovedUpstream) > 0 {
		fmt.Fprintf(out, "%d script(s) listed in provision.include are no longer in the upstream repo (your local copies will still run):\n", len(d.RemovedUpstream))
		for _, name := range d.RemovedUpstream {
			fmt.Fprintf(out, "  - %s\n", name)
		}
		fmt.Fprintln(out)
	}
}


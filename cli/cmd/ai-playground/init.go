package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/buildflow"
	"github.com/rodrigomideac/ai-playground/internal/config"
	"github.com/rodrigomideac/ai-playground/internal/doctor"
	"github.com/rodrigomideac/ai-playground/internal/paths"
	"github.com/rodrigomideac/ai-playground/internal/promptio"
	"github.com/rodrigomideac/ai-playground/internal/repo"
	"github.com/rodrigomideac/ai-playground/internal/ui"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup: writes config.yaml and populates build/. Does not run Packer.",
	Long: `init walks first-time users through doctor checks, picking which provision
scripts to include, and seeding the build/ directory under
$XDG_CONFIG_HOME/ai-playground. Run 'ai-playground build' afterwards
to actually produce the golden image.

When config.yaml is already present and build/ is missing, init populates
build/ headlessly using the values in config.yaml.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := contextWithTimeout(cmd.Context(), 10*time.Minute)
		defer cancel()
		return runInit(ctx, cmd.OutOrStdout())
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(ctx context.Context, out io.Writer) error {
	p := cliCtx.Paths

	if err := requireSupportedDistro(out); err != nil {
		return err
	}

	configPresent := p.ConfigExists()
	buildPresent := p.BuildPopulated()

	switch {
	case !configPresent && buildPresent:
		return fmt.Errorf("inconsistent state — run 'ai-playground reset'")
	case configPresent && buildPresent:
		ui.Detail("Already initialized — edit %s or run 'ai-playground reset'", p.Config)
		return nil
	case configPresent && !buildPresent:
		if err := runHeadlessPopulate(ctx, out, cliCtx.Cfg); err != nil {
			return err
		}
		ui.Success("Build inputs ready. Run 'ai-playground build' to produce the golden image.")
		return nil
	default: // !configPresent && !buildPresent
		return runHandholding(ctx, out)
	}
}

// requireSupportedDistro errors out on non-arch/debian/fedora hosts.
func requireSupportedDistro(out io.Writer) error {
	done := ui.Step("Detecting Linux distribution")
	d, raw, err := paths.DetectDistro()
	if err != nil {
		return err
	}
	if d == paths.DistroUnknown {
		ui.Fail("Detected unsupported distro: %s", raw)
		return fmt.Errorf("ai-playground supports Arch/Debian/Fedora families; detected: %s", raw)
	}
	done("Detected %s family (%s)", d, raw)
	return nil
}

// runHeadlessPopulate is the shared "config present, build/ missing" path.
// Called from `init` (where it's the final step) and from `build` (where it
// preceeds the actual Packer run). The caller decides what success message
// to print after.
func runHeadlessPopulate(ctx context.Context, out io.Writer, cfg *config.Config) error {
	doneSrc := ui.Step("Resolving repo source")
	src, err := repo.Resolve(ctx, repoOverride(), cliCtx.Paths.RepoCache)
	if err != nil {
		return err
	}
	doneSrc(sourceLabel(src))

	donePop := ui.Step("Populating %s from repo source", cliCtx.Paths.BuildDir)
	if err := buildflow.Populate(cliCtx.Paths, src, cfg, cfg.Provision.Include); err != nil {
		return err
	}
	donePop("")
	return nil
}

// sourceLabel renders a human-friendly description of the repo source.
func sourceLabel(src *repo.Source) string {
	if src.IsOverride {
		return fmt.Sprintf("Using override at %s", src.Path)
	}
	return fmt.Sprintf("Cached at %s", src.Path)
}

func runHandholding(ctx context.Context, out io.Writer) error {
	if !promptio.IsTTY() {
		return fmt.Errorf("init requires an interactive terminal. Pre-populate config.yaml and run 'ai-playground build' for headless setup.")
	}

	ui.Banner("ai-playground first-time setup")

	doneSrc := ui.Step("Resolving repo source")
	src, err := repo.Resolve(ctx, repoOverride(), cliCtx.Paths.RepoCache)
	if err != nil {
		return err
	}
	doneSrc(sourceLabel(src))

	doneDoc := ui.Step("Running doctor checks")
	if probs := doctor.Run(ctx, false); len(probs) > 0 {
		doctor.PrintProblems(out, probs)
		return fmt.Errorf("doctor checks failed")
	}
	doneDoc("All host checks passed")

	ui.Banner("Pick which setup scripts to include in the image")
	prompt := promptio.New()
	accepted, err := perScriptApproval(prompt, src.ProvisionDir())
	if err != nil {
		return err
	}

	ui.Banner("Worker VM identity")
	defaultUser := currentUsername()
	vmUser, err := prompt.Line("Username inside each worker VM", defaultUser)
	if err != nil {
		return err
	}
	if vmUser == "" {
		return fmt.Errorf("vm_user cannot be empty")
	}

	cfg := &config.Config{
		VMUser:     vmUser,
		Provision:  config.Provision{Include: accepted},
		OnConflict: config.OnConflictKeep,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	donePop := ui.Step("Populating %s", cliCtx.Paths.BuildDir)
	if err := buildflow.Populate(cliCtx.Paths, src, cfg, accepted); err != nil {
		return err
	}
	donePop("")

	doneCfg := ui.Step("Writing config.yaml")
	if err := os.MkdirAll(cliCtx.Paths.ConfigDir, 0o755); err != nil {
		return err
	}
	if err := config.Save(cliCtx.Paths.Config, cfg); err != nil {
		return err
	}
	doneCfg(cliCtx.Paths.Config)
	cliCtx.Cfg = cfg

	printCustomizationTip(out, cliCtx.Paths)
	return nil
}

// perScriptApproval walks <repo>/packer/provision/*.sh in numeric order,
// printing each script's `# Description:` header and asking [Y/n].
// Returns the filenames the user accepted, in source order.
func perScriptApproval(p *promptio.IO, provisionDir string) ([]string, error) {
	entries, err := os.ReadDir(provisionDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", provisionDir, err)
	}
	type script struct {
		name string
		desc string
	}
	var scripts []script
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		desc, _ := buildflow.ScriptDescription(filepath.Join(provisionDir, e.Name()))
		scripts = append(scripts, script{name: e.Name(), desc: desc})
	}
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].name < scripts[j].name })

	var accepted []string
	for _, s := range scripts {
		prefix := numericPrefix(s.name)
		fmt.Fprintf(p.Out, "\n  %s %s\n",
			ui.Bold("["+prefix+"]"),
			ui.Bold(strings.TrimPrefix(s.name, prefix+"-")))
		if s.desc != "" {
			fmt.Fprintf(p.Out, "      %s\n", ui.Dim(s.desc))
		}
		ok, err := p.YesNo("  Include this in your build?", true)
		if err != nil {
			return nil, err
		}
		if ok {
			accepted = append(accepted, s.name)
		}
	}
	return accepted, nil
}

func numericPrefix(name string) string {
	for i, r := range name {
		if r < '0' || r > '9' {
			return name[:i]
		}
	}
	return name
}

func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "vm"
}

func printCustomizationTip(out io.Writer, p *paths.Paths) {
	ui.Banner("Setup complete")
	fmt.Fprintf(out, `Build inputs are in:
  %s

Edit anything here before building. In particular:
  %s   add or edit setup scripts that run during the build
  %s   files placed here land in each worker's home directory

Run %s when you're ready.
`,
		ui.Bold(p.BuildDir),
		ui.Bold("provision/"),
		ui.Bold("chroot/etc/skel/"),
		ui.Bold("'ai-playground build'"))
}

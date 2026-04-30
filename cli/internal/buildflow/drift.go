package buildflow

import (
	"os"
	"sort"
	"strings"

	"github.com/rodrigomideac/ai-playground/cli/internal/config"
	"github.com/rodrigomideac/ai-playground/cli/internal/repo"
)

// Drift summarizes how the user's provision.include differs from the set of
// scripts shipped in the repo source's packer/provision/ directory.
type Drift struct {
	NewUpstream     []string // present in repo source, not in cfg.Provision.Include
	RemovedUpstream []string // listed in cfg.Provision.Include, missing in repo source
}

// Detect compares cfg.Provision.Include against the source's provision dir.
func Detect(src *repo.Source, cfg *config.Config) (*Drift, error) {
	repoScripts, err := listScripts(src.ProvisionDir())
	if err != nil {
		return nil, err
	}
	included := map[string]bool{}
	for _, name := range cfg.Provision.Include {
		included[name] = true
	}
	repoSet := map[string]bool{}
	for _, name := range repoScripts {
		repoSet[name] = true
	}
	var d Drift
	for _, name := range repoScripts {
		if !included[name] {
			d.NewUpstream = append(d.NewUpstream, name)
		}
	}
	for _, name := range cfg.Provision.Include {
		if !repoSet[name] {
			d.RemovedUpstream = append(d.RemovedUpstream, name)
		}
	}
	return &d, nil
}

func listScripts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".sh") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

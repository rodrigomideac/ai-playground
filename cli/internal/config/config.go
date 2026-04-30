// Package config reads and writes the declarative build spec at
// $XDG_CONFIG_HOME/ai-playground/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// OnConflict policy values.
const (
	OnConflictKeep      = "keep"
	OnConflictOverwrite = "overwrite"
)

// Provision holds the script-selection block.
type Provision struct {
	Include []string `yaml:"include"`
}

// Config is the on-disk shape of config.yaml.
type Config struct {
	VMUser     string    `yaml:"vm_user"`
	Provision  Provision `yaml:"provision"`
	OnConflict string    `yaml:"on_conflict,omitempty"`
}

// Validate checks invariants required by `init` and `build`.
func (c *Config) Validate() error {
	if c.VMUser == "" {
		return errors.New("vm_user is required")
	}
	switch c.OnConflict {
	case "", OnConflictKeep, OnConflictOverwrite:
	default:
		return fmt.Errorf("on_conflict must be %q or %q, got %q",
			OnConflictKeep, OnConflictOverwrite, c.OnConflict)
	}
	return nil
}

// Effective returns OnConflict with the default applied.
func (c *Config) Effective() string {
	if c.OnConflict == "" {
		return OnConflictKeep
	}
	return c.OnConflict
}

// Load reads and validates the config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	return &c, nil
}

// MaybeLoad returns (nil, nil) when the file does not exist.
func MaybeLoad(path string) (*Config, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// Save writes the config to path, creating it if needed (mode 0644).
func Save(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

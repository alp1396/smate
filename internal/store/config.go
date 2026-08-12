package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigName is the project config, kept in the repository root.
const ConfigName = ".smate.yml"

type Config struct {
	Image   string   `yaml:"image"`
	Secrets []string `yaml:"secrets,omitempty"`
	Mounts  []string `yaml:"mounts,omitempty"`
}

func LoadConfig(repo string) (Config, bool, error) {
	data, err := os.ReadFile(filepath.Join(repo, ConfigName))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read %s: %w", ConfigName, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", ConfigName, err)
	}
	return c, true, nil
}

func SaveConfig(repo string, c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode %s: %w", ConfigName, err)
	}
	if err := os.WriteFile(filepath.Join(repo, ConfigName), data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", ConfigName, err)
	}
	return nil
}

package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents a parsed wh.toml file.
type Config struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// LoadConfig parses a wh.toml file into a Config struct.
// For now, it uses a simple string-splitting parser for minimal dependencies.
func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, "wh.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read wh.toml: %w", err)
	}

	config := &Config{
		Dependencies: make(map[string]string),
	}

	lines := strings.Split(string(data), "\n")
	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.Trim(line, "[]")
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch currentSection {
		case "package":
			if key == "name" {
				config.Name = val
			} else if key == "version" {
				config.Version = val
			}
		case "dependencies":
			config.Dependencies[key] = val
		}
	}

	if config.Name == "" {
		return nil, fmt.Errorf("missing package.name in wh.toml")
	}

	return config, nil
}

// SaveConfig writes a Config struct to a wh.toml file.
func SaveConfig(dir string, config *Config) error {
	path := filepath.Join(dir, "wh.toml")
	var out strings.Builder

	out.WriteString("[package]\n")
	out.WriteString(fmt.Sprintf("name = %q\n", config.Name))
	out.WriteString(fmt.Sprintf("version = %q\n", config.Version))
	
	if len(config.Dependencies) > 0 {
		out.WriteString("\n[dependencies]\n")
		for k, v := range config.Dependencies {
			out.WriteString(fmt.Sprintf("%s = %q\n", k, v))
		}
	}

	return os.WriteFile(path, []byte(out.String()), 0644)
}

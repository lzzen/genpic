// Package mvpconfig reads the mvp_lite section of config.yaml for mvplite and
// genpic (default Base URL for the embedded web UI, optional listen port).
package mvpconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// mvpLiteYAML is the mvp_lite section of config.yaml.
type mvpLiteYAML struct {
	Port           string `yaml:"port"`
	DefaultBaseURL string `yaml:"default_base_url"`
}

// rootYAML is a minimal parse of config.yaml so unknown keys from the full
// platform example file are ignored.
type rootYAML struct {
	MvpLite mvpLiteYAML `yaml:"mvp_lite"`
}

// Read loads MVP Lite settings from a YAML file. Missing file is not an error
// (found=false).
func Read(path string) (port, defaultBaseURL string, found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	var root rootYAML
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", "", true, fmt.Errorf("parse %s: %w", path, err)
	}
	port = strings.TrimSpace(root.MvpLite.Port)
	defaultBaseURL = strings.TrimSpace(root.MvpLite.DefaultBaseURL)
	return port, defaultBaseURL, true, nil
}

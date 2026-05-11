// Package mvpconfig reads shared UI defaults and per-binary listen ports from
// config.yaml: mvp_lite.default_base_url (GET /api/public-config), mvp_lite.port
// (cmd/mvplite only), server.port (cmd/genpic only), optional model_id_map.
package mvpconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// serverYAML is the server section (full platform listen port).
type serverYAML struct {
	Port string `yaml:"port"`
}

// mvpLiteYAML is the mvp_lite section of config.yaml.
type mvpLiteYAML struct {
	Port           string `yaml:"port"`
	DefaultBaseURL string `yaml:"default_base_url"`
}

// rootYAML is a minimal parse of config.yaml so unknown keys from the full
// platform example file are ignored.
type rootYAML struct {
	Server     serverYAML        `yaml:"server"`
	MvpLite    mvpLiteYAML       `yaml:"mvp_lite"`
	ModelIDMap map[string]string `yaml:"model_id_map"`
}

// Config holds parsed settings from config.yaml for mvplite and genpic.
// Found is false when the file does not exist; otherwise Found is true even if
// all fields are empty.
type Config struct {
	Found          bool
	MvpLitePort    string // cmd/mvplite: mvp_lite.port
	ServerPort     string // cmd/genpic: server.port
	DefaultBaseURL string // mvp_lite.default_base_url (both binaries)
	// ModelIDMap maps catalog or wire model id to the upstream model string.
	ModelIDMap map[string]string
}

// Read loads MVP Lite and server.port settings from a YAML file. Missing file
// is not an error (Found=false).
func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var root rootYAML
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return Config{
		Found:          true,
		MvpLitePort:    strings.TrimSpace(root.MvpLite.Port),
		ServerPort:     strings.TrimSpace(root.Server.Port),
		DefaultBaseURL: strings.TrimSpace(root.MvpLite.DefaultBaseURL),
		ModelIDMap:     stringMapOrNil(root.ModelIDMap),
	}, nil
}

func stringMapOrNil(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

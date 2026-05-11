// Package modelmap maps catalog or wire model IDs to upstream wire IDs (config.yaml model_id_map).
package modelmap

import "strings"

// Apply returns the upstream model string to send: if m is non-empty and any
// candidate key (trimmed, tried in order) has a non-empty mapped value, that
// value is returned; otherwise defaultWire is returned.
func Apply(m map[string]string, candidates []string, defaultWire string) string {
	if len(m) == 0 {
		return defaultWire
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if v, ok := m[c]; ok {
			if out := strings.TrimSpace(v); out != "" {
				return out
			}
		}
	}
	return defaultWire
}

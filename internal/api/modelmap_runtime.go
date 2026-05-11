package api

import (
	"sync"
)

var (
	modelIDMapMu sync.RWMutex
	modelIDMap   map[string]string
)

// SetModelIDMap installs config.yaml model_id_map entries (nil or empty clears).
// Call once at process startup from cmd/genpic.
func SetModelIDMap(m map[string]string) {
	modelIDMapMu.Lock()
	defer modelIDMapMu.Unlock()
	if len(m) == 0 {
		modelIDMap = nil
		return
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	modelIDMap = cp
}

func getModelIDMap() map[string]string {
	modelIDMapMu.RLock()
	defer modelIDMapMu.RUnlock()
	return modelIDMap
}

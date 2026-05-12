package api

import (
	"net/http"
	"time"

	"genpic/pkg/provider"
)

// HandleListModels serves GET /models.
// It returns the flat model list from the provider registry, shaped to match
// the OpenAI /v1/models response so OpenAI-compatible clients work out of the box.
func HandleListModels(w http.ResponseWriter, r *http.Request) {
	models := provider.AllModels()

	type modelObj struct {
		ID           string   `json:"id"`
		Object       string   `json:"object"`
		Created      int64    `json:"created"`
		OwnedBy      string   `json:"owned_by"`
		Capabilities []string `json:"capabilities,omitempty"`
	}

	data := make([]modelObj, 0, len(models))
	for _, m := range models {
		caps := make([]string, len(m.Capabilities))
		for i, c := range m.Capabilities {
			caps[i] = string(c)
		}
		data = append(data, modelObj{
			ID:           m.ID,
			Object:       "model",
			Created:      time.Now().Unix(),
			OwnedBy:      "genpic",
			Capabilities: caps,
		})
	}

	JSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

package api

import "sync/atomic"

var xiangyunUICatalogEnabled atomic.Bool

// SetXiangyunUICatalog controls whether GET /api/ui/catalog includes the 祥云 vendor.
func SetXiangyunUICatalog(enabled bool) {
	xiangyunUICatalogEnabled.Store(enabled)
}

func xiangyunUICatalogOn() bool {
	return xiangyunUICatalogEnabled.Load()
}

package drivers

import (
	"testing"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
)

func TestDefaultRegistryIncludesBuiltInDrivers(t *testing.T) {
	registry := DefaultRegistry()
	cases := []struct {
		driver       string
		wantCategory string
	}{
		{"mysql", engine.CategorySQL},
		{"oceanbase", engine.CategorySQL},
		{"redis", engine.CategoryKV},
	}
	for _, tc := range cases {
		got, ok := registry.Category(config.DatasourceConfig{Driver: tc.driver})
		if !ok {
			t.Fatalf("driver %q not registered", tc.driver)
		}
		if got != tc.wantCategory {
			t.Fatalf("driver %q category = %q, want %q", tc.driver, got, tc.wantCategory)
		}
	}
}

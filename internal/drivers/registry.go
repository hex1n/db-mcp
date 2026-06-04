package drivers

import (
	"github.com/hex1n/db-mcp/internal/engine"
	"github.com/hex1n/db-mcp/internal/engine/mysqlengine"
	"github.com/hex1n/db-mcp/internal/engine/redisengine"
)

func DefaultRegistry() *engine.Registry {
	registrations := mysqlengine.Registrations()
	registrations = append(registrations, redisengine.Registrations()...)
	return engine.NewRegistry(registrations...)
}

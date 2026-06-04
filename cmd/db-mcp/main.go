package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime/debug"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hex1n/db-mcp/internal/app"
	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/drivers"
	"github.com/hex1n/db-mcp/internal/mcpserver"
)

var version = "dev"

func main() {
	configPath := flag.String("config", config.DefaultPath(), "path to db-mcp TOML config")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	resolvedVersion := resolveVersion()
	if *showVersion {
		fmt.Println("db-mcp " + resolvedVersion)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	application := app.New(cfg, *configPath, drivers.DefaultRegistry())
	defer application.Close()

	server := mcpserver.New(application, resolvedVersion)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func resolveVersion() string {
	if value := strings.TrimSpace(version); value != "" && value != "dev" {
		return strings.TrimPrefix(value, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if value := strings.TrimSpace(info.Main.Version); value != "" && value != "(devel)" {
			return strings.TrimPrefix(value, "v")
		}
	}
	return version
}

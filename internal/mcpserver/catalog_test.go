package mcpserver

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hex1n/db-mcp/internal/config"
	"github.com/hex1n/db-mcp/internal/engine"
)

func TestToolCatalogSQLOnly(t *testing.T) {
	specs := buildToolCatalog(config.Config{
		Mode: config.ModeOperate,
		Datasources: map[string]config.DatasourceConfig{
			"sql": {Driver: "mysql"},
		},
	}, map[string]bool{engine.CategorySQL: true})

	got := toolSpecNames(specs)
	want := []string{
		toolCurrentDatasource,
		toolDescribeTable,
		toolExecuteSQL,
		toolGetCurrentTime,
		toolListDatasources,
		toolListTables,
		toolSampleTable,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool catalog names = %v, want %v", got, want)
	}
}

func TestToolCatalogRedisOnly(t *testing.T) {
	specs := buildToolCatalog(config.Config{
		Mode: config.ModeOperate,
		Datasources: map[string]config.DatasourceConfig{
			"redis": {Driver: "redis"},
		},
	}, map[string]bool{engine.CategoryKV: true})

	got := toolSpecNames(specs)
	want := []string{
		toolCurrentDatasource,
		toolGetCurrentTime,
		toolListDatasources,
		toolRedisCommand,
		toolRedisGet,
		toolRedisScan,
		toolRedisTTL,
		toolRedisType,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool catalog names = %v, want %v", got, want)
	}
}

func TestToolCatalogHighRiskDatasourceRequirement(t *testing.T) {
	cfg := config.Config{
		Mode: config.ModeOperate,
		Datasources: map[string]config.DatasourceConfig{
			"sql":   {Driver: "mysql"},
			"redis": {Driver: "redis"},
		},
	}
	specs := toolByName(buildToolCatalog(cfg, map[string]bool{engine.CategorySQL: true, engine.CategoryKV: true}))

	for _, name := range []string{toolExecuteSQL, toolRedisCommand} {
		if !specs[name].RequireDatasource {
			t.Fatalf("%s should require datasource when multiple datasources are configured", name)
		}
	}
	if specs[toolListTables].RequireDatasource || specs[toolRedisGet].RequireDatasource {
		t.Fatalf("read tools must not require datasource at schema level: %+v %+v", specs[toolListTables], specs[toolRedisGet])
	}
}

func TestToolCatalogModeDescriptions(t *testing.T) {
	inspect := toolByName(buildToolCatalog(config.Config{
		Mode: config.ModeInspect,
		Datasources: map[string]config.DatasourceConfig{
			"sql": {Driver: "mysql"},
		},
	}, map[string]bool{engine.CategorySQL: true}))
	if desc := inspect[toolExecuteSQL].Description; !strings.Contains(desc, "Inspect mode is ON") {
		t.Fatalf("inspect execute_sql description = %q", desc)
	}

	operate := toolByName(buildToolCatalog(config.Config{
		Mode: config.ModeOperate,
		Datasources: map[string]config.DatasourceConfig{
			"redis": {Driver: "redis"},
		},
	}, map[string]bool{engine.CategoryKV: true}))
	if desc := operate[toolRedisCommand].Description; !strings.Contains(desc, "Operate mode is ON") {
		t.Fatalf("operate redis_command description = %q", desc)
	}
}

func TestToolCatalogAnnotations(t *testing.T) {
	specs := toolByName(buildToolCatalog(config.Config{
		Mode: config.ModeOperate,
		Datasources: map[string]config.DatasourceConfig{
			"sql":   {Driver: "mysql"},
			"redis": {Driver: "redis"},
		},
	}, map[string]bool{engine.CategorySQL: true, engine.CategoryKV: true}))

	for _, name := range []string{toolListDatasources, toolCurrentDatasource, toolGetCurrentTime, toolListTables, toolRedisGet} {
		spec := specs[name]
		if spec.Annotations == nil || !spec.Annotations.ReadOnlyHint {
			t.Fatalf("%s should be read-only, got %+v", name, spec.Annotations)
		}
		if spec.Annotations.DestructiveHint != nil && *spec.Annotations.DestructiveHint {
			t.Fatalf("%s should not be destructive, got %+v", name, spec.Annotations)
		}
	}

	for _, name := range []string{toolExecuteSQL, toolRedisCommand} {
		spec := specs[name]
		if spec.Annotations == nil || spec.Annotations.ReadOnlyHint {
			t.Fatalf("%s should not be read-only, got %+v", name, spec.Annotations)
		}
		if spec.Annotations.DestructiveHint == nil || !*spec.Annotations.DestructiveHint {
			t.Fatalf("%s should be destructive, got %+v", name, spec.Annotations)
		}
	}
}

func toolSpecNames(specs []toolSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	sort.Strings(names)
	return names
}

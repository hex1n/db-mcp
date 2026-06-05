package mcpserver

type datasourceToolInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
}

type executeSQLInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; required when multiple datasources are configured"`
	SQL        string `json:"sql" jsonschema:"single SQL statement to execute"`
	MaxRows    int    `json:"max_rows,omitempty" jsonschema:"maximum rows returned for query statements"`
}

type executeSQLRequiredDatasourceInput struct {
	Datasource string `json:"datasource" jsonschema:"datasource name"`
	SQL        string `json:"sql" jsonschema:"single SQL statement to execute"`
	MaxRows    int    `json:"max_rows,omitempty" jsonschema:"maximum rows returned for query statements"`
}

type tableToolInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Table      string `json:"table" jsonschema:"table name"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum sample rows"`
}

type redisKeyInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Key        string `json:"key" jsonschema:"redis key"`
}

type redisScanInput struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"datasource name; defaults to config.default"`
	Pattern    string `json:"pattern,omitempty" jsonschema:"MATCH pattern; defaults to *"`
	Count      int    `json:"count,omitempty" jsonschema:"maximum keys to return"`
}

type redisCommandInput struct {
	Datasource string   `json:"datasource,omitempty" jsonschema:"datasource name; required when multiple datasources are configured"`
	Command    []string `json:"command" jsonschema:"redis command as argv, e.g. [\"GET\",\"foo\"]"`
}

type redisCommandRequiredDatasourceInput struct {
	Datasource string   `json:"datasource" jsonschema:"datasource name"`
	Command    []string `json:"command" jsonschema:"redis command as argv, e.g. [\"GET\",\"foo\"]"`
}

// genschema writes the ClickHouse schema that the MCP server serves as resources
// (pkg/dashboard/schema/*.txt), built from the migrations of bylonis-otel-collector.
//
// It is a separate module on purpose: bylonis-otel-collector is AGPL-3.0 and the MCP
// server is Apache-2.0, so the server binary embeds only the generated text and never
// links the collector. Regenerate after a collector release that changes the schema:
//
//	cd tools/genschema && go run . ../../pkg/dashboard/schema
//
// CI runs the same command and fails if the committed files are stale.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	schemamigrator "github.com/bylonis/bylonis-otel-collector/cmd/bylonisschemamigrator/schema_migrator"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genschema <output dir>")
		os.Exit(2)
	}
	for _, signal := range []string{"logs", "metrics", "traces"} {
		path := filepath.Join(os.Args[1], signal+".txt")
		if err := os.WriteFile(path, []byte(clickHouseSchema(signal)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

var essentialTables = map[string][]string{
	"bylonis_logs": {
		"logs_v2",
		"distributed_logs_v2",
		"logs_v2_resource",
		"distributed_logs_v2_resource",
		"tag_attributes_v2",
		"distributed_tag_attributes_v2",
	},
	"bylonis_metrics": {
		"samples_v4",
		"distributed_samples_v4",
		"time_series_v4",
		"distributed_time_series_v4",
		"time_series_v4_6hrs",
		"distributed_time_series_v4_6hrs",
		"time_series_v4_1day",
		"distributed_time_series_v4_1day",
		"exp_hist",
		"distributed_exp_hist",
	},
	"bylonis_traces": {
		"bylonis_index_v3",
		"distributed_bylonis_index_v3",
		"traces_v3_resource",
		"distributed_traces_v3_resource",
		"tag_attributes_v2",
		"distributed_tag_attributes_v2",
		"dependency_graph_minutes_v2",
		"distributed_dependency_graph_minutes_v2",
		"trace_summary",
		"distributed_trace_summary",
		"bylonis_error_index_v2",
		"distributed_bylonis_error_index_v2",
		"top_level_operations",
		"distributed_top_level_operations",
		"span_attributes_keys",
		"distributed_span_attributes_keys",
	},
}

type TableSchema struct {
	Name    string
	Columns []schemamigrator.Column
	Indexes []schemamigrator.Index
	Engine  any
}

// clickHouseSchema returns the ClickHouse schema for a signal ("logs", "metrics" or "traces").
func clickHouseSchema(signal string) string {
	var buf bytes.Buffer

	switch signal {
	case "logs":
		buf.WriteString("\n=== LOGS SCHEMA ===\n")
		exportSchema(&buf, "bylonis_logs", getLogsMigrations())
	case "metrics":
		buf.WriteString("\n=== METRICS SCHEMA ===\n")
		exportSchema(&buf, "bylonis_metrics", getMetricsMigrations())
	case "traces":
		buf.WriteString("\n=== TRACES SCHEMA ===\n")
		exportSchema(&buf, "bylonis_traces", getTracesMigrations())
	default:
		buf.WriteString(fmt.Sprintf("Error: unknown signal type '%s'. Use 'logs', 'metrics', or 'traces'\n", signal))
	}

	return buf.String()
}

func getLogsMigrations() []schemamigrator.SchemaMigrationRecord {
	allMigrations := append(schemamigrator.SquashedLogsMigrations, schemamigrator.LogsMigrations...)
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].MigrationID < allMigrations[j].MigrationID
	})
	return allMigrations
}

func getMetricsMigrations() []schemamigrator.SchemaMigrationRecord {
	allMigrations := append(schemamigrator.SquashedMetricsMigrations, schemamigrator.MetricsMigrations...)
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].MigrationID < allMigrations[j].MigrationID
	})
	return allMigrations
}

func getTracesMigrations() []schemamigrator.SchemaMigrationRecord {
	allMigrations := append(schemamigrator.SquashedTracesMigrations, schemamigrator.TracesMigrations...)
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].MigrationID < allMigrations[j].MigrationID
	})
	return allMigrations
}

func exportSchema(buf *bytes.Buffer, database string, migrations []schemamigrator.SchemaMigrationRecord) {
	tables := make(map[string]*TableSchema)

	for _, migration := range migrations {
		for _, op := range migration.UpItems {
			applyOperation(tables, op)
		}
	}

	filteredTables := filterEssentialTables(database, tables)

	outputSummary(buf, database, filteredTables)
}

func applyOperation(tables map[string]*TableSchema, op schemamigrator.Operation) {
	switch operation := op.(type) {
	case schemamigrator.CreateTableOperation:
		tables[operation.Table] = &TableSchema{
			Name:    operation.Table,
			Columns: operation.Columns,
			Indexes: operation.Indexes,
			Engine:  operation.Engine,
		}
	case schemamigrator.CreateTableAsOperation:
		if source, exists := tables[operation.AsTable]; exists {
			tables[operation.Table] = &TableSchema{
				Name:    operation.Table,
				Columns: append([]schemamigrator.Column(nil), source.Columns...),
				Indexes: append([]schemamigrator.Index(nil), source.Indexes...),
				Engine:  operation.Engine,
			}
		}
	case schemamigrator.RenameTableOperation:
		if table, exists := tables[operation.Table]; exists {
			delete(tables, operation.Table)
			table.Name = operation.NewTable
			tables[operation.NewTable] = table
		}
	case schemamigrator.DropTableOperation:
		delete(tables, operation.Table)
	case schemamigrator.AlterTableAddColumn:
		if table, exists := tables[operation.Table]; exists {
			table.Columns = append(table.Columns, operation.Column)
		}
	case schemamigrator.AlterTableDropColumn:
		if table, exists := tables[operation.Table]; exists {
			newColumns := []schemamigrator.Column{}
			for _, col := range table.Columns {
				if col.Name != operation.Column.Name {
					newColumns = append(newColumns, col)
				}
			}
			table.Columns = newColumns
		}
	}
}

func filterEssentialTables(database string, tables map[string]*TableSchema) map[string]*TableSchema {
	essential, exists := essentialTables[database]
	if !exists {
		return tables
	}

	filtered := make(map[string]*TableSchema)
	for _, tableName := range essential {
		if table, exists := tables[tableName]; exists {
			filtered[tableName] = table
		}
	}

	return filtered
}

func outputSummary(buf *bytes.Buffer, database string, tables map[string]*TableSchema) {
	fmt.Fprintf(buf, "\nDatabase: %s\n\n", database)
	fmt.Fprintf(buf, "Total Tables: %d\n\n", len(tables))

	tableNames := make([]string, 0, len(tables))
	for name := range tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	var localTables, distributedTables []string
	for _, name := range tableNames {
		if strings.HasPrefix(name, "distributed_") {
			distributedTables = append(distributedTables, name)
		} else {
			localTables = append(localTables, name)
		}
	}

	if len(localTables) > 0 {
		buf.WriteString("Local Tables\n\n")
		for _, tableName := range localTables {
			outputTableDetails(buf, tables[tableName])
		}
	}

	if len(distributedTables) > 0 {
		buf.WriteString("Distributed Tables\n\n")
		for _, tableName := range distributedTables {
			outputTableDetails(buf, tables[tableName])
		}
	}
}

func outputTableDetails(buf *bytes.Buffer, table *TableSchema) {
	fmt.Fprintf(buf, "Table: %s\n\n", table.Name)

	fmt.Fprintf(buf, "- Engine: %s\n", getEngineType(table.Engine))
	fmt.Fprintf(buf, "- Total Columns: %d\n", len(table.Columns))
	fmt.Fprintf(buf, "- Total Indexes: %d\n\n", len(table.Indexes))

	if len(table.Columns) > 0 {
		buf.WriteString("Columns:\n")
		for _, col := range table.Columns {
			colType := formatColumnType(col.Type)
			details := []string{}

			if col.Default != "" {
				details = append(details, fmt.Sprintf("Default: %s", col.Default))
			}
			if col.Alias != "" {
				details = append(details, fmt.Sprintf("Alias: %s", col.Alias))
			}
			if col.TTL != "" {
				details = append(details, fmt.Sprintf("TTL: %s", col.TTL))
			}
			if col.Materialized != "" {
				details = append(details, fmt.Sprintf("Materialized: %s", col.Materialized))
			}

			line := fmt.Sprintf("- %s: %s", col.Name, colType)
			if len(details) > 0 {
				line += fmt.Sprintf(" - %s", strings.Join(details, ", "))
			}
			buf.WriteString(line + "\n")
		}
		buf.WriteString("\n")
	}

	if len(table.Indexes) > 0 {
		buf.WriteString("Indexes:\n")
		for _, idx := range table.Indexes {
			fmt.Fprintf(buf, "- %s (%s) - Type: %s\n", idx.Name, idx.Expression, idx.Type)
		}
		buf.WriteString("\n")
	}

	buf.WriteString("---\n\n")
}

func formatColumnType(colType any) string {
	switch t := colType.(type) {
	case schemamigrator.LowCardinalityColumnType:
		return fmt.Sprintf("LowCardinality(%s)", formatColumnType(t.ElementType))
	case schemamigrator.MapColumnType:
		return fmt.Sprintf("Map(%s, %s)", formatColumnType(t.KeyType), formatColumnType(t.ValueType))
	case schemamigrator.ArrayColumnType:
		return fmt.Sprintf("Array(%s)", formatColumnType(t.ElementType))
	case schemamigrator.NullableColumnType:
		return fmt.Sprintf("Nullable(%s)", formatColumnType(t.ElementType))
	case schemamigrator.FixedStringColumnType:
		return fmt.Sprintf("FixedString(%d)", t.Length)
	case schemamigrator.DateTime64ColumnType:
		if t.Timezone != "" {
			return fmt.Sprintf("DateTime64(%d, '%s')", t.Precision, t.Timezone)
		}
		return fmt.Sprintf("DateTime64(%d)", t.Precision)
	case schemamigrator.DateTimeColumnType:
		if t.Timezone != "" {
			return fmt.Sprintf("DateTime('%s')", t.Timezone)
		}
		return "DateTime"
	case schemamigrator.SimpleAggregateFunction:
		args := make([]string, len(t.Arguments))
		for i, arg := range t.Arguments {
			args[i] = formatColumnType(arg)
		}
		return fmt.Sprintf("SimpleAggregateFunction(%s, %s)", t.FunctionName, strings.Join(args, ", "))
	case schemamigrator.AggregateFunction:
		args := make([]string, len(t.Arguments))
		for i, arg := range t.Arguments {
			args[i] = formatColumnType(arg)
		}
		return fmt.Sprintf("AggregateFunction(%s, %s)", t.FunctionName, strings.Join(args, ", "))
	case schemamigrator.JSONColumnType:
		return t.String()
	case schemamigrator.TupleColumnType:
		elements := make([]string, len(t.ElementTypes))
		for i, et := range t.ElementTypes {
			elements[i] = formatColumnType(et)
		}
		return fmt.Sprintf("Tuple(%s)", strings.Join(elements, ", "))
	case schemamigrator.EnumerationColumnType:
		return fmt.Sprintf("Enum%d(%s)", t.Size, strings.Join(t.Values, ", "))
	default:
		return fmt.Sprintf("%v", t)
	}
}

func getEngineType(engine any) string {
	switch engine.(type) {
	case schemamigrator.MergeTree:
		return "MergeTree"
	case schemamigrator.ReplacingMergeTree:
		return "ReplacingMergeTree"
	case schemamigrator.AggregatingMergeTree:
		return "AggregatingMergeTree"
	case schemamigrator.SummingMergeTree:
		return "SummingMergeTree"
	case schemamigrator.Distributed:
		return "Distributed"
	default:
		return "Unknown"
	}
}

package dashboard

import (
	"regexp"
	"testing"
)

func TestTracesSchemaUsesByLonisTableNames(t *testing.T) {
	InitClickhouseSchema()
	for _, table := range []string{
		"bylonis_index_v3",
		"distributed_bylonis_index_v3",
		"bylonis_error_index_v2",
		"distributed_bylonis_error_index_v2",
	} {
		if !regexp.MustCompile(`(?m)^Table: ` + table + `$`).MatchString(TracesSchema) {
			t.Errorf("traces schema is missing table %s", table)
		}
	}
	if loc := regexp.MustCompile(`(?i)signoz`).FindStringIndex(TracesSchema); loc != nil {
		t.Errorf("traces schema mentions signoz: …%s…", TracesSchema[max(loc[0]-60, 0):min(loc[1]+60, len(TracesSchema))])
	}
}

func TestTracesQueryExamplesUseByLonisTableNames(t *testing.T) {
	if regexp.MustCompile(`(?i)signoz`).MatchString(ClickhouseSqlQueryForTraces) {
		t.Error("traces query examples mention signoz")
	}
}

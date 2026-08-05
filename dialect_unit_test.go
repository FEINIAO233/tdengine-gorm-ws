package tdengine_gorm

import (
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/using"
	"github.com/taosdata/driver-go/v3/common"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestBindVarToEncodesTDengineStrings(t *testing.T) {
	dialect := Dialect{}
	input := "O'Reilly\\sensor\n"
	stmt := &gorm.Statement{Vars: []interface{}{input}}
	var placeholder strings.Builder

	dialect.BindVarTo(&placeholder, stmt, input)
	if placeholder.String() != "?" {
		t.Fatalf("expected placeholder ?, got %q", placeholder.String())
	}

	encoded, ok := stmt.Vars[0].([]byte)
	if !ok {
		t.Fatalf("expected encoded []byte, got %T", stmt.Vars[0])
	}
	actual, err := common.InterpolateParams("SELECT ?", []driver.NamedValue{{Ordinal: 1, Value: encoded}})
	if err != nil {
		t.Fatalf("interpolate value: %v", err)
	}
	const expected = "SELECT 'O\\'Reilly\\\\sensor\\n'"
	if actual != expected {
		t.Fatalf("expected %q, got %q", expected, actual)
	}
	if explained := dialect.Explain("SELECT ?", encoded); explained != expected {
		t.Fatalf("expected explained SQL %q, got %q", expected, explained)
	}
}

func TestBindVarToPreservesTime(t *testing.T) {
	dialect := Dialect{}
	now := time.Date(2026, time.August, 4, 12, 0, 0, 123456789, time.UTC)
	stmt := &gorm.Statement{Vars: []interface{}{now}}
	var placeholder strings.Builder

	dialect.BindVarTo(&placeholder, stmt, now)
	if actual, ok := stmt.Vars[0].(time.Time); !ok || !actual.Equal(now) {
		t.Fatalf("expected time.Time to be preserved, got %#v", stmt.Vars[0])
	}
}

func TestQuoteTo(t *testing.T) {
	tests := map[string]string{
		"metrics":       "`metrics`",
		"power.metrics": "`power`.`metrics`",
		"select":        "`select`",
		"device-1":      "`device-1`",
	}

	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			var quoted strings.Builder
			Dialect{}.QuoteTo(&quoted, input)
			if quoted.String() != expected {
				t.Fatalf("expected %q, got %q", expected, quoted.String())
			}
		})
	}
}

func TestTDengine3UsingSQL(t *testing.T) {
	db, err := gorm.Open(Open("root:taosdata@ws(127.0.0.1:6041)/power"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}

	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	result := db.Table("device-1").Clauses(using.SetUsingTags(
		"select",
		using.Tag{Name: "location", Value: "north'west"},
		using.Tag{Name: "group", Value: 7},
	)).Create(map[string]interface{}{"ts": now, "val": 12.5})
	if result.Error != nil {
		t.Fatalf("build insert: %v", result.Error)
	}

	const expectedSQL = "INSERT INTO `device-1` USING `select`(`location`,`group`) TAGS(?,?) (`ts`,`val`) VALUES (?,?)"
	if result.Statement.SQL.String() != expectedSQL {
		t.Fatalf("expected %q, got %q", expectedSQL, result.Statement.SQL.String())
	}

	args := make([]driver.NamedValue, len(result.Statement.Vars))
	for index, value := range result.Statement.Vars {
		args[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	actual, err := common.InterpolateParams(result.Statement.SQL.String(), args)
	if err != nil {
		t.Fatalf("interpolate SQL: %v", err)
	}
	if !strings.Contains(actual, "TAGS('north\\'west',7)") {
		t.Fatalf("expected quoted tag value, got %q", actual)
	}
}

func TestUnsignedDataTypes(t *testing.T) {
	dialect := Dialect{}
	tests := []struct {
		size     int
		expected string
	}{
		{size: 8, expected: "tinyint unsigned"},
		{size: 16, expected: "smallint unsigned"},
		{size: 32, expected: "int unsigned"},
		{size: 64, expected: "bigint unsigned"},
	}

	for _, test := range tests {
		field := &schema.Field{DataType: schema.Uint, Size: test.size}
		if actual := dialect.DataTypeOf(field); actual != test.expected {
			t.Fatalf("size %d: expected %q, got %q", test.size, test.expected, actual)
		}
	}
}

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

	encoded, ok := stmt.Vars[0].(sqlLiteral)
	if !ok {
		t.Fatalf("expected encoded SQL literal, got %T", stmt.Vars[0])
	}
	driverValue, err := encoded.Value()
	if err != nil {
		t.Fatalf("convert encoded value: %v", err)
	}
	actual, err := common.InterpolateParams("SELECT ?", []driver.NamedValue{{Ordinal: 1, Value: driverValue}})
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

func TestBindVarToPreparedModePreservesStrings(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		config  *gorm.Config
	}{
		{
			name:    "DSN disables interpolation",
			dialect: Dialect{DSN: "root:taosdata@ws(127.0.0.1:6041)/power?interpolateParams=false"},
			config:  &gorm.Config{DryRun: true},
		},
		{
			name:    "GORM prepares statements",
			dialect: Dialect{DSN: "root:taosdata@ws(127.0.0.1:6041)/power"},
			config:  &gorm.Config{DryRun: true, PrepareStmt: true},
		},
		{
			name:    "explicit prepared mode",
			dialect: Dialect{DSN: "root:taosdata@ws(127.0.0.1:6041)/power", BindMode: BindModePrepared},
			config:  &gorm.Config{DryRun: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(&test.dialect, test.config)
			if err != nil {
				t.Fatalf("open dry-run database: %v", err)
			}
			result := db.Table("metrics").Create(map[string]interface{}{"note": "raw", "ts": time.Now()})
			if result.Error != nil {
				t.Fatalf("build prepared insert: %v", result.Error)
			}
			if value, ok := result.Statement.Vars[0].(string); !ok || value != "raw" {
				t.Fatalf("expected raw string, got %#v", result.Statement.Vars[0])
			}
		})
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
		if literal, ok := value.(sqlLiteral); ok {
			var err error
			value, err = literal.Value()
			if err != nil {
				t.Fatalf("convert SQL literal: %v", err)
			}
		}
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

func TestTDengineBatchValuesSQL(t *testing.T) {
	db, err := gorm.Open(Open("root:taosdata@ws(127.0.0.1:6041)/power"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	timestamps := []time.Time{
		time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 5, 12, 0, 1, 0, time.UTC),
	}
	result := db.Table("metrics").Create([]map[string]interface{}{
		{"note": "first", "ts": timestamps[0], "val": 1.5},
		{"note": "second", "ts": timestamps[1], "val": 2.5},
	})
	if result.Error != nil {
		t.Fatalf("build batch insert: %v", result.Error)
	}
	const expected = "INSERT INTO `metrics` (`note`,`ts`,`val`) VALUES (?,?,?) (?,?,?)"
	if result.Statement.SQL.String() != expected {
		t.Fatalf("expected %q, got %q", expected, result.Statement.SQL.String())
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

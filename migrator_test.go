package tdengine_gorm

import (
	"errors"
	"testing"
	"time"

	createclause "github.com/FEINIAO233/tdengine-gorm-ws/clause/create"
	"gorm.io/gorm"
)

type invalidMigrationModel struct {
	Value float64
	TS    time.Time
}

type validIndexModel struct {
	TS       time.Time
	Location string `gorm:"index:idx_location" tdengine:"tag"`
}

type invalidIndexModel struct {
	TS    time.Time
	Value float64 `gorm:"index:idx_value"`
}

type invalidJSONColumnModel struct {
	TS      time.Time
	Payload string `gorm:"type:JSON"`
}

type invalidDecimalTagModel struct {
	TS    time.Time
	Price string `gorm:"type:DECIMAL;precision:18;scale:2" tdengine:"tag"`
}

type compositeKeyModel struct {
	TS       time.Time
	DeviceID string `gorm:"type:VARCHAR;size:64" tdengine:"compositeKey"`
	Value    float64
}

type invalidCompositeKeyTypeModel struct {
	TS    time.Time
	Value float64 `tdengine:"compositeKey"`
}

type duplicateCompositeKeyModel struct {
	TS       time.Time
	DeviceID string `gorm:"type:VARCHAR;size:64" tdengine:"compositeKey"`
	Sequence int    `tdengine:"compositeKey"`
}

func TestMigratorRequiresTimestampFirst(t *testing.T) {
	db := openDryRunDB(t, nil)
	err := db.Table("invalid_metrics").Migrator().CreateTable(&invalidMigrationModel{})
	if !errors.Is(err, ErrTimestampFirst) {
		t.Fatalf("expected ErrTimestampFirst, got %v", err)
	}
}

func TestTagIndexValidation(t *testing.T) {
	db := openDryRunDB(t, nil)
	for _, test := range []struct {
		model interface{}
		err   error
	}{{&validIndexModel{}, nil}, {&invalidIndexModel{}, ErrTagIndexOnly}} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(test.model); err != nil {
			t.Fatalf("parse index model: %v", err)
		}
		err := validateModelIndexes(stmt.Schema)
		if !errors.Is(err, test.err) {
			t.Fatalf("expected %v, got %v", test.err, err)
		}
	}
}

func TestValidateDataColumns(t *testing.T) {
	if err := validateDataColumns(nil); !errors.Is(err, ErrNoDataColumns) {
		t.Fatalf("expected ErrNoDataColumns, got %v", err)
	}
	if err := validateDataColumns([]*createclause.Column{{Name: "ts", ColumnType: "timestamp(6)"}}); err != nil {
		t.Fatalf("expected timestamp precision to be accepted: %v", err)
	}
}

func TestExtendedTypeValidation(t *testing.T) {
	db := openDryRunDB(t, nil)
	for _, model := range []interface{}{&invalidJSONColumnModel{}, &invalidDecimalTagModel{}} {
		if err := db.Table("invalid_types").Migrator().CreateTable(model); err == nil {
			t.Fatalf("expected TDengine type validation to reject %T", model)
		}
	}
}

func TestCompositeKeyValidation(t *testing.T) {
	db := openDryRunDB(t, nil)
	for _, test := range []struct {
		model interface{}
		err   error
	}{
		{model: &compositeKeyModel{}},
		{model: &invalidCompositeKeyTypeModel{}, err: ErrCompositeKeyInvalid},
		{model: &duplicateCompositeKeyModel{}, err: ErrCompositeKeyInvalid},
	} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(test.model); err != nil {
			t.Fatalf("parse composite key model: %v", err)
		}
		columns, tags := (Migrator{d: Dialect{}}).modelColumns(stmt.Schema)
		err := validateModelColumns(columns, tags)
		if !errors.Is(err, test.err) {
			t.Fatalf("%T: expected %v, got %v", test.model, test.err, err)
		}
	}
}

func TestVirtualTableTypeDetection(t *testing.T) {
	for tableType, expected := range map[string]bool{
		"NORMAL_TABLE":         false,
		"CHILD_TABLE":          false,
		"SUPER_TABLE":          false,
		"VIRTUAL_NORMAL_TABLE": true,
		"VIRTUAL_CHILD_TABLE":  true,
		"virtual super table":  true,
	} {
		if actual := isVirtualTableType(tableType); actual != expected {
			t.Fatalf("%q: expected %v, got %v", tableType, expected, actual)
		}
	}
}

func TestUnsupportedMigratorOperationsAreExplicit(t *testing.T) {
	migrator := openDryRunDB(t, nil).Migrator()
	if err := migrator.CreateConstraint(&validIndexModel{}, "fk_invalid"); !errors.Is(err, ErrConstraintsUnsupported) {
		t.Fatalf("expected ErrConstraintsUnsupported, got %v", err)
	}
	if err := migrator.RenameTable("old_metrics", "new_metrics"); !errors.Is(err, ErrRenameTableUnsupported) {
		t.Fatalf("expected ErrRenameTableUnsupported, got %v", err)
	}
	if migrator.HasConstraint(&validIndexModel{}, "fk_invalid") {
		t.Fatal("TDengine must not report GORM constraints")
	}
}

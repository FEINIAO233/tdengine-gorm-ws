package tdengine_gorm

import (
	"errors"
	"testing"
	"time"

	createclause "github.com/FEINIAO233/tdengine-gorm-ws/clause/create"
)

type invalidMigrationModel struct {
	Value float64
	TS    time.Time
}

func TestMigratorRequiresTimestampFirst(t *testing.T) {
	db := openDryRunDB(t, nil)
	err := db.Table("invalid_metrics").Migrator().CreateTable(&invalidMigrationModel{})
	if !errors.Is(err, ErrTimestampFirst) {
		t.Fatalf("expected ErrTimestampFirst, got %v", err)
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

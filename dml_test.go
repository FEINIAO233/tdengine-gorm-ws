package tdengine_gorm

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func openDryRunDB(t *testing.T, config *gorm.Config) *gorm.DB {
	t.Helper()
	if config == nil {
		config = &gorm.Config{}
	}
	config.DryRun = true
	db, err := gorm.Open(Open("root:taosdata@ws(127.0.0.1:6041)/power"), config)
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	return db
}

func TestUpdateIsExplicitlyBlocked(t *testing.T) {
	db := openDryRunDB(t, nil)
	result := db.Table("metrics").Where("ts = ?", time.Now()).Update("val", 2)
	if !errors.Is(result.Error, ErrUpdateNotSupported) {
		t.Fatalf("expected ErrUpdateNotSupported, got %v", result.Error)
	}
}

func TestDeleteIsExplicitlyBlocked(t *testing.T) {
	db := openDryRunDB(t, nil)
	result := db.Table("metrics").Where("ts < ?", time.Now()).Delete(map[string]interface{}{})
	if !errors.Is(result.Error, ErrDeleteNotSupported) {
		t.Fatalf("expected ErrDeleteNotSupported, got %v", result.Error)
	}
}

func TestOnConflictIsExplicitlyBlocked(t *testing.T) {
	result := openDryRunDB(t, nil).Table("metrics").Clauses(clause.OnConflict{DoNothing: true}).Create(map[string]interface{}{
		"ts": time.Now(), "val": 1,
	})
	if !errors.Is(result.Error, ErrOnConflictUnsupported) {
		t.Fatalf("expected ErrOnConflictUnsupported, got %v", result.Error)
	}
}

func TestDeleteTimeRange(t *testing.T) {
	db := openDryRunDB(t, nil)
	start := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	result := DeleteTimeRange(db, "power.device-1", &start, &end)
	if result.Error != nil {
		t.Fatalf("build delete range: %v", result.Error)
	}
	const expected = "DELETE FROM `power`.`device-1` WHERE _rowts >= ? AND _rowts < ?"
	if result.Statement.SQL.String() != expected {
		t.Fatalf("expected %q, got %q", expected, result.Statement.SQL.String())
	}
	if len(result.Statement.Vars) != 2 {
		t.Fatalf("expected two time bounds, got %d", len(result.Statement.Vars))
	}
}

func TestDeleteTimeRangeRequiresBound(t *testing.T) {
	result := DeleteTimeRange(openDryRunDB(t, nil), "metrics", nil, nil)
	if !errors.Is(result.Error, ErrDeleteRangeRequired) {
		t.Fatalf("expected ErrDeleteRangeRequired, got %v", result.Error)
	}
}

package tdengine_gorm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/create"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/fill"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/interp"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/partition"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/using"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/window"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func integrationEndpoint(t *testing.T) string {
	t.Helper()
	endpoint := strings.TrimRight(os.Getenv("TDENGINE_GORM_TEST_ENDPOINT"), "/")
	if endpoint == "" {
		t.Skip("set TDENGINE_GORM_TEST_ENDPOINT to run TDengine integration tests")
	}
	return endpoint
}

type integrationDatabase struct {
	DB  *gorm.DB
	DSN string
}

func openIntegrationDatabase(t *testing.T) integrationDatabase {
	t.Helper()
	endpoint := integrationEndpoint(t)
	server, err := sql.Open(DriverName, endpoint+"/")
	if err != nil {
		t.Fatalf("open TDengine server: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = server.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for TDengine: %v", err)
		}
		time.Sleep(time.Second)
	}

	database := fmt.Sprintf("gorm_v2_%d", time.Now().UnixNano())
	if _, err = server.Exec("CREATE DATABASE " + database); err != nil {
		t.Fatalf("create integration database: %v", err)
	}
	t.Cleanup(func() { _, _ = server.Exec("DROP DATABASE IF EXISTS " + database) })

	dsn := endpoint + "/" + database + "?timezone=UTC"
	db, err := gorm.Open(Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration connection pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return integrationDatabase{DB: db, DSN: dsn}
}

func supportsPreparedStatements(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var version string
	if err := db.Raw("SELECT SERVER_VERSION()").Scan(&version).Error; err != nil {
		t.Fatalf("query TDengine server version: %v", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		t.Fatalf("parse TDengine server version %q: %v", version, err)
	}
	return major > 3 || major == 3 && minor >= 4
}

func TestDialectIntegration(t *testing.T) {
	db := openIntegrationDatabase(t).DB

	var value int
	if err := db.Raw("SELECT 1").Scan(&value).Error; err != nil {
		t.Fatalf("query TDengine: %v", err)
	}
	if value != 1 {
		t.Fatalf("expected 1, got %d", value)
	}
	if err := db.Exec("SELECT * RFOM missing_table").Error; err == nil {
		t.Fatal("expected invalid SQL to fail")
	}
}

func TestTDengine3Integration(t *testing.T) {
	integration := openIntegrationDatabase(t)
	db := integration.DB

	stable := create.NewSTable("select", true, []*create.Column{
		{Name: "ts", ColumnType: create.TimestampType},
		{Name: "val", ColumnType: create.DoubleType},
		{Name: "note", ColumnType: create.NCharType, Length: 64},
	}, []*create.Column{
		{Name: "location", ColumnType: create.NCharType, Length: 64},
		{Name: "group", ColumnType: create.IntType},
	})
	if err := db.Table("select").Clauses(create.NewCreateTableClause([]*create.Table{stable})).Create(map[string]interface{}{}).Error; err != nil {
		t.Fatalf("create supertable: %v", err)
	}

	firstTable := create.NewTable("device-1", true, nil, "select", map[string]interface{}{
		"location": "north'west",
		"group":    7,
	})
	if err := db.Table("device-1").Clauses(create.NewCreateTableClause([]*create.Table{firstTable})).Create(map[string]interface{}{}).Error; err != nil {
		t.Fatalf("create subtable: %v", err)
	}

	timestamp := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Table("device-1").Create(map[string]interface{}{
		"ts": timestamp, "val": 12.5, "note": "O'Reilly",
	}).Error; err != nil {
		t.Fatalf("insert into existing subtable: %v", err)
	}

	secondTimestamp := timestamp.Add(time.Second)
	if err := db.Table("device-2").Clauses(using.SetUsingTags(
		"select",
		using.Tag{Name: "location", Value: "south"},
		using.Tag{Name: "group", Value: 8},
	)).Create(map[string]interface{}{
		"ts": secondTimestamp, "val": 18.25, "note": "automatic",
	}).Error; err != nil {
		t.Fatalf("create subtable while inserting: %v", err)
	}

	type measurement struct {
		TS       time.Time
		Value    float64 `gorm:"column:val"`
		Note     string
		Location string
		Group    int `gorm:"column:group"`
	}

	var first measurement
	if err := db.Table("select").Where("tbname = ? AND note = ?", "device-1", "O'Reilly").Take(&first).Error; err != nil {
		t.Fatalf("query escaped string: %v", err)
	}
	if first.Value != 12.5 || first.Note != "O'Reilly" || first.Location != "north'west" || first.Group != 7 {
		t.Fatalf("unexpected first row: %+v", first)
	}

	var second measurement
	if err := db.Table("select").Where("tbname = ? AND ts = ?", "device-2", secondTimestamp).Take(&second).Error; err != nil {
		t.Fatalf("query automatically created subtable: %v", err)
	}
	if second.Value != 18.25 || second.Note != "automatic" || second.Location != "south" || second.Group != 8 {
		t.Fatalf("unexpected second row: %+v", second)
	}

	batchStart := secondTimestamp.Add(time.Second)
	if err := db.Table("device-1").Create([]map[string]interface{}{
		{"ts": batchStart, "val": 20.5, "note": "batch-1"},
		{"ts": batchStart.Add(time.Second), "val": 21.5, "note": "batch-2"},
	}).Error; err != nil {
		t.Fatalf("batch insert: %v", err)
	}
	var batchCount int64
	if err := db.Table("device-1").Where("ts >= ?", batchStart).Count(&batchCount).Error; err != nil {
		t.Fatalf("count batch rows: %v", err)
	}
	if batchCount != 2 {
		t.Fatalf("expected 2 batch rows, got %d", batchCount)
	}

	var partitions []struct {
		Table string `gorm:"column:tbname"`
		Total int64  `gorm:"column:total"`
	}
	if err := db.Table("select").Clauses(partition.Columns("tbname")).
		Select("tbname, count(*) AS total").Scan(&partitions).Error; err != nil {
		t.Fatalf("partition query: %v", err)
	}
	if len(partitions) < 2 {
		t.Fatalf("expected at least two table partitions, got %v", partitions)
	}

	var countWindows []struct {
		Total int64 `gorm:"column:total"`
	}
	if err := db.Table("device-1").Clauses(window.SetCountWindow(2)).
		Select("count(*) AS total").Scan(&countWindows).Error; err != nil {
		t.Fatalf("count window query: %v", err)
	}
	if len(countWindows) != 2 {
		t.Fatalf("expected two count windows, got %v", countWindows)
	}

	var eventWindows []struct {
		Total int64 `gorm:"column:total"`
	}
	if err := db.Table("device-1").Clauses(window.SetEventWindow(
		clause.Expr{SQL: "val >= ?", Vars: []interface{}{20}},
		clause.Expr{SQL: "val >= ?", Vars: []interface{}{21}},
	)).Select("count(*) AS total").Scan(&eventWindows).Error; err != nil {
		t.Fatalf("event window query: %v", err)
	}
	if len(eventWindows) != 1 || eventWindows[0].Total != 2 {
		t.Fatalf("unexpected event windows: %v", eventWindows)
	}

	var interpolated []struct {
		Value float64 `gorm:"column:val"`
	}
	interpolationEnd := batchStart.Add(time.Second)
	if err := db.Table("device-1").Clauses(
		interp.SetRange(timestamp, interpolationEnd),
		interp.SetEvery(window.Duration{Value: 1, Unit: window.Second}),
		fill.SetFill(fill.FillLinear),
	).Select("interp(val) AS val").Scan(&interpolated).Error; err != nil {
		t.Fatalf("interpolation query: %v", err)
	}
	if len(interpolated) != 4 {
		t.Fatalf("expected four interpolated points, got %v", interpolated)
	}

	if supportsPreparedStatements(t, db) {
		preparedTable := create.NewTable("device-prepared", true, nil, "select", map[string]interface{}{
			"location": "prepared",
			"group":    9,
		})
		if err := db.Table("device-prepared").Clauses(create.NewCreateTableClause([]*create.Table{preparedTable})).Create(map[string]interface{}{}).Error; err != nil {
			t.Fatalf("create prepared subtable: %v", err)
		}
		preparedDB, err := gorm.Open(Open(integration.DSN), &gorm.Config{PrepareStmt: true})
		if err != nil {
			t.Fatalf("open prepared database: %v", err)
		}
		preparedSQLDB, err := preparedDB.DB()
		if err != nil {
			t.Fatalf("get prepared connection pool: %v", err)
		}
		t.Cleanup(func() { _ = preparedSQLDB.Close() })
		preparedTimestamp := batchStart.Add(2 * time.Second)
		if err := preparedDB.Table("device-prepared").Create(map[string]interface{}{
			"ts": preparedTimestamp, "val": 30.5, "note": "prepared O'Reilly",
		}).Error; err != nil {
			t.Fatalf("prepared insert: %v", err)
		}
		var prepared measurement
		if err := preparedDB.Table("select").Where("tbname = ? AND ts = ?", "device-prepared", preparedTimestamp).Take(&prepared).Error; err != nil {
			t.Fatalf("prepared query: %v", err)
		}
		if prepared.Value != 30.5 || prepared.Note != "prepared O'Reilly" {
			t.Fatalf("unexpected prepared row: %+v", prepared)
		}
	} else {
		t.Log("TDengine 3.3.x uses interpolation mode; prepared statement integration requires TDengine 3.4+")
	}

	deleteEnd := batchStart.Add(2 * time.Second)
	if err := DeleteTimeRange(db, "device-1", &batchStart, &deleteEnd).Error; err != nil {
		t.Fatalf("delete time range: %v", err)
	}
	if err := db.Table("device-1").Where("ts >= ? AND ts < ?", batchStart, deleteEnd).Count(&batchCount).Error; err != nil {
		t.Fatalf("count deleted range: %v", err)
	}
	if batchCount != 0 {
		t.Fatalf("expected deleted time range to be empty, got %d rows", batchCount)
	}
}

type autoMetricV1 struct {
	TS       time.Time `gorm:"column:ts"`
	Value    float64   `gorm:"column:val"`
	Location string    `gorm:"column:location;type:NCHAR(64)" tdengine:"tag"`
}

type autoMetricV2 struct {
	TS       time.Time `gorm:"column:ts"`
	Value    float64   `gorm:"column:val"`
	Note     string    `gorm:"column:note;type:NCHAR(64)"`
	Location string    `gorm:"column:location;type:NCHAR(64)" tdengine:"tag"`
	GroupID  int       `gorm:"column:group_id;index:idx_auto_group" tdengine:"tag"`
}

type plainMetricV1 struct {
	TS    time.Time `gorm:"column:ts"`
	Value float64   `gorm:"column:val"`
}

type plainMetricV2 struct {
	TS    time.Time `gorm:"column:ts"`
	Value float64   `gorm:"column:val"`
	Note  string    `gorm:"column:note;type:NCHAR(64)"`
}

func TestTDengineMigratorIntegration(t *testing.T) {
	db := openIntegrationDatabase(t).DB
	const stableName = "auto_metrics"

	if err := db.Table(stableName).AutoMigrate(&autoMetricV1{}); err != nil {
		t.Fatalf("create supertable with AutoMigrate: %v", err)
	}
	if !db.Migrator().HasTable(stableName) {
		t.Fatal("expected AutoMigrate to create supertable")
	}
	if !db.Migrator().HasColumn(stableName, "location") {
		t.Fatal("expected migrator to find tag metadata")
	}

	if err := db.Table(stableName).AutoMigrate(&autoMetricV2{}); err != nil {
		t.Fatalf("add column and tag with AutoMigrate: %v", err)
	}
	for _, name := range []string{"ts", "val", "note", "location", "group_id"} {
		if !db.Migrator().HasColumn(stableName, name) {
			t.Fatalf("expected migrated column or tag %q", name)
		}
	}
	if !db.Table(stableName).Migrator().HasIndex(&autoMetricV2{}, "idx_auto_group") {
		t.Fatal("expected AutoMigrate to create tag index")
	}
	indexes, err := db.Table(stableName).Migrator().GetIndexes(&autoMetricV2{})
	if err != nil {
		t.Fatalf("get tag indexes: %v", err)
	}
	foundIndex := false
	for _, index := range indexes {
		if index.Name() == "idx_auto_group" {
			foundIndex = true
			break
		}
	}
	if !foundIndex {
		t.Fatalf("expected idx_auto_group in indexes: %v", indexes)
	}
	stableMigrator := db.Table(stableName).Migrator()
	if err := stableMigrator.DropIndex(&autoMetricV2{}, "idx_auto_group"); err != nil {
		t.Fatalf("drop tag index: %v", err)
	}
	if stableMigrator.HasIndex(&autoMetricV2{}, "idx_auto_group") {
		t.Fatal("expected tag index to be dropped")
	}
	if err := stableMigrator.CreateIndex(&autoMetricV2{}, "idx_auto_group"); err != nil {
		t.Fatalf("recreate tag index: %v", err)
	}
	tableType, err := db.Migrator().TableType(stableName)
	if err != nil {
		t.Fatalf("get supertable type: %v", err)
	}
	if tableType.Name() != stableName || tableType.Type() != "SUPER TABLE" {
		t.Fatalf("unexpected supertable type: name=%q type=%q", tableType.Name(), tableType.Type())
	}
	columnTypes, err := db.Migrator().ColumnTypes(stableName)
	if err != nil {
		t.Fatalf("read TDengine column metadata: %v", err)
	}
	if len(columnTypes) < 3 {
		t.Fatalf("expected at least 3 data columns, got %d", len(columnTypes))
	}

	const plainTable = "plain_metrics"
	if err := db.Table(plainTable).AutoMigrate(&plainMetricV1{}); err != nil {
		t.Fatalf("create regular table with AutoMigrate: %v", err)
	}
	if err := db.Table(plainTable).AutoMigrate(&plainMetricV2{}); err != nil {
		t.Fatalf("add regular table column with AutoMigrate: %v", err)
	}
	if !db.Migrator().HasTable(plainTable) || !db.Migrator().HasColumn(plainTable, "note") {
		t.Fatal("expected regular table and added column in metadata")
	}

	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatalf("get tables: %v", err)
	}
	found := false
	for _, table := range tables {
		if table == stableName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in table list: %v", stableName, tables)
	}
}

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
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/using"
	"gorm.io/gorm"
)

func integrationEndpoint(t *testing.T) string {
	t.Helper()
	endpoint := strings.TrimRight(os.Getenv("TDENGINE_GORM_TEST_ENDPOINT"), "/")
	if endpoint == "" {
		t.Skip("set TDENGINE_GORM_TEST_ENDPOINT to run TDengine integration tests")
	}
	return endpoint
}

func openIntegrationDatabase(t *testing.T) *gorm.DB {
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

	db, err := gorm.Open(Open(endpoint+"/"+database+"?timezone=UTC"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration connection pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestDialectIntegration(t *testing.T) {
	db := openIntegrationDatabase(t)

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
	db := openIntegrationDatabase(t)

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
}

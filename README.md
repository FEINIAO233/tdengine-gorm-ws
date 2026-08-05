# TDengine GORM WebSocket Dialect

English | [简体中文](./README.zh-CN.md)

A GORM dialect for TDengine 3.x that connects to `taosAdapter` through the
official `driver-go` WebSocket driver. It does not require the local TDengine C
client.

## Compatibility

- Go 1.18+
- GORM 1.31.x
- `driver-go/v3` 3.8.x
- TDengine 3.3.6+ (CI covers 3.3.8.8 and 3.4.1.6)

Transactions and regular SQL `UPDATE` statements are not supported. The
dialect supports safe additive migrations, batch inserts, guarded time-range
deletes, supertables, subtables, tag indexes, and TDengine-specific query
clauses.

See [MIGRATION.md](./MIGRATION.md) when upgrading from `v0.2.0`.

## Installation

```bash
go get github.com/FEINIAO233/tdengine-gorm-ws@latest
```

## Connection

```go
package main

import (
	"log"

	tdengine "github.com/FEINIAO233/tdengine-gorm-ws"
	"gorm.io/gorm"
)

func main() {
	dsn := "root:taosdata@ws(127.0.0.1:6041)/metrics?timezone=Asia%2FShanghai"
	db, err := gorm.Open(tdengine.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	_ = db
}
```

Use `wss(host:port)` for TLS connections. URL-encode usernames or passwords
containing special characters according to the `driver-go` DSN rules.

The default mode uses `driver-go` parameter interpolation. The following
options preserve raw Go values and use prepared statements:

```go
db, err := gorm.Open(tdengine.Open(dsn), &gorm.Config{PrepareStmt: true})

// Or let driver-go prepare parameters through the DSN.
dsn = dsn + "&interpolateParams=false"
```

You can also explicitly configure
`&tdengine.Dialect{DSN: dsn, BindMode: tdengine.BindModePrepared}`.

For TDengine 3.3.x, keep the default interpolation mode. With the current
`driver-go` Prepared/Stmt2 path, TDengine 3.4+ is the prepared-statement
compatibility baseline. CI runs prepared-statement integration tests only on
3.4.x.

## Supertables and Subtables

```go
import (
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/create"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/using"
)

stable := create.NewSTable("meters", true, []*create.Column{
	{Name: "ts", ColumnType: create.TimestampType},
	{Name: "val", ColumnType: create.DoubleType},
}, []*create.Column{
	{Name: "location", ColumnType: create.NCharType, Length: 64},
})

err := db.Table("meters").
	Clauses(create.NewCreateTableClause([]*create.Table{stable})).
	Create(map[string]interface{}{}).Error

err = db.Table("device-1").Clauses(using.SetUsingTags(
	"meters",
	using.Tag{Name: "location", Value: "Shanghai"},
)).Create(map[string]interface{}{
	"ts": time.Now(), "val": 12.5,
}).Error
```

`SetUsing` still accepts `map[string]interface{}` and sorts tags by name to
produce deterministic SQL. Use `SetUsingTags` when tag order must be explicit.

## Batch Inserts

Pass a slice directly to GORM. The dialect emits TDengine's consecutive row
syntax, `VALUES (...) (...)`:

```go
err := db.Table("device-1").Create([]map[string]interface{}{
	{"ts": time.Now(), "val": 12.5},
	{"ts": time.Now().Add(time.Second), "val": 13.5},
}).Error
```

## Auto Migration

`AutoMigrate` is additive by design. It creates missing regular tables or
supertables and adds missing columns and tags. It never drops columns or tags
and never changes existing types automatically. The first data field must map
to `TIMESTAMP`; mark tags with `tdengine:"tag"`:

```go
type Meter struct {
	TS       time.Time `gorm:"column:ts"`
	Value    float64   `gorm:"column:val"`
	Location string    `gorm:"type:NCHAR(64)" tdengine:"tag"`
}

err := db.Table("meters").AutoMigrate(&Meter{})
```

For explicit DDL changes, assert `db.Migrator()` to `tdengine.Migrator` and use
methods such as `AddStableColumn`, `AddStableTag`, `ModifyStableTag`, and
`RenameStableTag`.

### Tag Indexes

TDengine permits an index on a single supertable tag. Standard GORM index tags
participate in `AutoMigrate`:

```go
type Meter struct {
	TS       time.Time `gorm:"column:ts"`
	Location string    `gorm:"index:idx_meter_location" tdengine:"tag"`
}
```

`CreateIndex`, `DropIndex`, `HasIndex`, and `GetIndexes` use
`INFORMATION_SCHEMA.INS_INDEXES`. Indexes on regular columns, multi-column
indexes, unique indexes, and index renaming return explicit errors. TDengine
automatically indexes the first tag of a supertable; migration does not create
a duplicate when the same tag is already indexed.

### Extended Data Types

Use GORM type tags for TDengine 3.x data types:

```go
type ExtendedMetric struct {
	TS       time.Time
	Name     string `gorm:"type:VARCHAR;size:128"`
	Raw      []byte `gorm:"type:VARBINARY;size:256"`
	Price    string `gorm:"type:DECIMAL;precision:18;scale:2"`
	Geometry string `gorm:"type:GEOMETRY;size:512"`
	Payload  []byte `gorm:"type:BLOB"`
	Metadata string `gorm:"type:JSON" tdengine:"tag"`
}
```

The migrator enforces relevant TDengine restrictions: JSON is tag-only,
DECIMAL and BLOB cannot be tags, and a table can contain at most one BLOB
column.

DECIMAL requires TDengine 3.3.6+. BLOB and column-filtered `COUNT_WINDOW`
require TDengine 3.3.7+. Enable `PrepareStmt` or `BindModePrepared` when writing
raw `[]byte` values to VARBINARY or BLOB columns so binary data is not handled
as an interpolated string.

## Updates and Deletes

TDengine does not provide regular row-level `UPDATE`. GORM `Update` and
`Updates` return `ErrUpdateNotSupported`; update a row by inserting the same
timestamp again.

Generic GORM `Delete` returns `ErrDeleteNotSupported` to guard against
irreversible deletes. Use a bounded time range instead:

```go
start := time.Now().Add(-time.Hour)
end := time.Now()
err := tdengine.DeleteTimeRange(db, "device-1", &start, &end).Error
```

The start is inclusive and the end is exclusive. Either bound may be `nil`,
but they cannot both be `nil`.

## TDengine Query Extensions

The library provides clauses for:

- `CREATE TABLE` / `CREATE STABLE`
- `USING ... TAGS`
- `INTERVAL`, `SESSION`, `STATE_WINDOW`, `EVENT_WINDOW`, and `COUNT_WINDOW`
- `PARTITION BY`
- `RANGE` / `EVERY` for INTERP queries
- `FILL`
- `SLIMIT` / `SOFFSET`

See [example/example.go](./example/example.go) for complete examples.

## Integration Tests

Start TDengine and taosAdapter, then set a WebSocket endpoint without a
database name:

```powershell
$env:TDENGINE_GORM_TEST_ENDPOINT='root:taosdata@ws(127.0.0.1:6041)'
go test -v -count=1 -run 'Integration$' .
```

The tests create and remove temporary databases automatically. GitHub Actions
runs the same suite against TDengine 3.3.8.8 and 3.4.1.6.

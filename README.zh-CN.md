# TDengine GORM WebSocket 方言

[English](./README.md) | 简体中文

面向 TDengine 3.x 的 GORM 方言，通过官方 `driver-go` WebSocket 驱动连接
`taosAdapter`，不依赖本地 TDengine C 客户端。

## 兼容范围

- Go 1.18+
- GORM 1.31.x
- `driver-go/v3` 3.8.x
- TDengine 3.3.6+（CI 覆盖 3.3.8.8、3.4.1.6 和 3.4.2.2）

当前不支持事务和普通 SQL `UPDATE`。支持安全的增量自动迁移、批量写入和受时间范围保护的删除；超级表、子表以及 TDengine 查询扩展通过本库提供的 clauses 使用。

从 `v0.2.0` 升级请阅读 [MIGRATION.md](./MIGRATION.md)。

## 安装

```bash
go get github.com/FEINIAO233/tdengine-gorm-ws@latest
```

## 连接

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

TLS 连接使用 `wss(host:port)`。用户名或密码包含特殊字符时，应按照
`driver-go` DSN 规则进行 URL 转义。

默认使用 `driver-go` 的参数插值模式。以下两种写法会保留原始 Go 值并使用预编译语句：

```go
db, err := gorm.Open(tdengine.Open(dsn), &gorm.Config{PrepareStmt: true})

// 或通过 DSN 交给 driver-go 自动预编译
dsn = dsn + "&interpolateParams=false"
```

也可以构造 `&tdengine.Dialect{DSN: dsn, BindMode: tdengine.BindModePrepared}` 显式指定模式。

TDengine 3.3.x 建议保持默认参数插值模式。基于当前 `driver-go` 的 Prepared/Stmt2 集成路径以 TDengine 3.4+ 为兼容基线；CI 只在 3.4.x 执行 Prepared Statement 真实服务测试。

## 超级表与子表

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

`SetUsing` 仍接受 `map[string]interface{}`，并会按标签名排序以生成稳定 SQL；需要明确标签顺序时使用 `SetUsingTags`。

TDengine Migrator 支持一次修改一个子表的多个 Tag，也支持批量修改多个子表：

```go
migrator := db.Migrator().(tdengine.Migrator)

err = migrator.SetTableTags("device-1", map[string]interface{}{
	"location": "Beijing",
	"group":    8,
})

err = migrator.SetTableTagsBatch(
	tdengine.TableTagUpdate{Table: "device-1", Tags: map[string]interface{}{"group": 9}},
	tdengine.TableTagUpdate{Table: "device-2", Tags: map[string]interface{}{"group": 10}},
)
```

多子表批量修改需要 TDengine 3.4+。Tag 名称会排序，以生成稳定 SQL。

## 批量写入

直接向 GORM 传入 slice。本方言会生成 TDengine 要求的连续行语法 `VALUES (...) (...)`：

```go
err := db.Table("device-1").Create([]map[string]interface{}{
	{"ts": time.Now(), "val": 12.5},
	{"ts": time.Now().Add(time.Second), "val": 13.5},
}).Error
```

## 自动迁移

`AutoMigrate` 只执行安全的增量操作：创建普通表或超级表，以及新增缺失的列和标签。它不会自动删除列、标签或修改类型。结构体的第一个数据字段必须映射为 `TIMESTAMP`，标签使用 `tdengine:"tag"` 标记：

```go
type Meter struct {
	TS       time.Time `gorm:"column:ts"`
	Value    float64   `gorm:"column:val"`
	Location string    `gorm:"type:NCHAR(64)" tdengine:"tag"`
}

err := db.Table("meters").AutoMigrate(&Meter{})
```

TDengine 可以使用时间戳和另一个整数或 `VARCHAR` 字段组成复合主键。使用
`tdengine:"compositeKey"` 显式标记第二个字段：

```go
type DeviceMetric struct {
	TS       time.Time
	DeviceID string `gorm:"type:VARCHAR;size:64" tdengine:"compositeKey"`
	Value    float64
}
```

复合主键只能在首次建表时声明。对已有表执行 `AutoMigrate` 时，本库会返回
`ErrCompositeKeyMigrationUnsupported`，不会尝试修改已有主键。

对已有定义执行显式 DDL 时，可将 `db.Migrator()` 断言为 `tdengine.Migrator`，使用 `AddStableColumn`、`AddStableTag`、`ModifyStableTag`、`RenameStableTag` 等方法。

TDengine 虚拟表可以正常查询。涉及结构修改的 Migrator 操作会识别虚拟表并返回
`ErrVirtualTableUnsupported`；管理虚拟表定义时应显式执行 TDengine `VTABLE` SQL。

### 表参数与压缩参数

底层建表 clause 支持 TDengine 表参数和字段级压缩配置：

```go
table := create.NewTable("metrics", true, []*create.Column{
	{
		Name: "ts", ColumnType: create.TimestampType,
		Encode: create.EncodeDeltaI, Compress: create.CompressLZ4,
		Level: create.CompressionMedium,
	},
	{
		Name: "value", ColumnType: create.DoubleType,
		Encode: create.EncodeBSS, Compress: create.CompressZstd,
		Level: create.CompressionHigh,
	},
}, "", nil).
	WithComment("device metrics").
	WithSMA("value").
	WithTTL(30)
```

普通表和子表使用 `WithTTL`；超级表使用 `WithKeep(value, unit)`，例如
`WithKeep(365, create.RetentionDays)`。不合法的参数组合或压缩算法会在生成 SQL 时返回明确错误。

### Tag Index

TDengine 只允许在超级表的单个 Tag 上建立索引。使用标准 GORM index tag 即可参与 `AutoMigrate`：

```go
type Meter struct {
	TS       time.Time `gorm:"column:ts"`
	Location string    `gorm:"index:idx_meter_location" tdengine:"tag"`
}
```

`CreateIndex`、`DropIndex`、`HasIndex` 和 `GetIndexes` 已针对 `INFORMATION_SCHEMA.INS_INDEXES` 适配。普通列索引、多列索引、唯一索引和索引重命名会明确返回错误。TDengine 会自动为超级表的第一个 Tag 建立索引；自动迁移检测到同一 Tag 已有索引时不会重复创建。

### 扩展字段类型

通过 GORM 类型标签可使用 TDengine 3.x 扩展类型：

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

迁移器会校验 TDengine 限制：JSON 只能作为 Tag，DECIMAL 和 BLOB 不能作为 Tag，每张表最多一个 BLOB 字段。

DECIMAL 需要 TDengine 3.3.6+，BLOB 和带列过滤的 `COUNT_WINDOW` 需要 TDengine 3.3.7+。
写入 VARBINARY/BLOB 的原始 `[]byte` 时应启用 `PrepareStmt` 或 `BindModePrepared`，避免把二进制内容当作插值字符串处理。

## 更新与删除

TDengine 没有普通行级 `UPDATE`。`db.Update` / `db.Updates` 会返回 `ErrUpdateNotSupported`；更新数据应重新插入相同时间戳。

GORM `ON CONFLICT` clause 会返回 `ErrOnConflictUnsupported`。TDengine 通过时间戳或复合主键的重复写入语义处理冲突，不使用 PostgreSQL 风格的冲突子句。

普通 GORM `Delete` 会返回 `ErrDeleteNotSupported`，避免误发不可逆删除。按时间范围删除时使用：

```go
start := time.Now().Add(-time.Hour)
end := time.Now()
err := tdengine.DeleteTimeRange(db, "device-1", &start, &end).Error
```

开始时间包含、结束时间不包含；两端可有一端为 `nil`，但不能同时为空。

## TDengine 查询扩展

本库提供以下 clauses：

- `CREATE TABLE` / `CREATE STABLE`
- `USING ... TAGS`
- `INTERVAL`、`SESSION`、`STATE_WINDOW`、`EVENT_WINDOW`、`COUNT_WINDOW`
- `PARTITION BY`
- INTERP 查询的 `RANGE` / `EVERY`
- `FILL`
- `SLIMIT` / `SOFFSET`

完整示例见 [example/example.go](./example/example.go)。

## 集成测试

启动 TDengine 和 taosAdapter 后设置不包含数据库名的 WebSocket endpoint：

```powershell
$env:TDENGINE_GORM_TEST_ENDPOINT='root:taosdata@ws(127.0.0.1:6041)'
go test -v -count=1 -run 'Integration$' .
```

测试会创建临时数据库，并在结束时自动删除。GitHub Actions 会使用 TDengine 3.3.8.8、3.4.1.6 和 3.4.2.2 执行同一套测试。

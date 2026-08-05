# TDengine GORM WebSocket Dialect

面向 TDengine 3.x 的 GORM 方言，通过官方 `driver-go` WebSocket 驱动连接
`taosAdapter`，不依赖本地 TDengine C 客户端。

## 兼容范围

- Go 1.18+
- GORM 1.31.x
- `driver-go/v3` 3.8.x
- TDengine 3.3.6+（CI 覆盖 3.3.8.8 和 3.4.1.6）

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

对已有定义执行显式 DDL 时，可将 `db.Migrator()` 断言为 `tdengine.Migrator`，使用 `AddStableColumn`、`AddStableTag`、`ModifyStableTag`、`RenameStableTag` 等方法。

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

测试会创建临时数据库，并在结束时自动删除。GitHub Actions 会使用 TDengine 3.3.8.8 和 3.4.1.6 执行同一套测试。

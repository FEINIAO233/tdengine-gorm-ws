# TDengine GORM WebSocket Dialect

面向 TDengine 3.x 的 GORM 方言，通过官方 `driver-go` WebSocket 驱动连接
`taosAdapter`，不依赖本地 TDengine C 客户端。

## 兼容范围

- Go 1.18+
- GORM 1.31.x
- `driver-go/v3` 3.8.x
- TDengine 3.3.6+（CI 基线为 3.4.1.6）

当前不支持事务、自动迁移、更新和删除。超级表、子表以及 TDengine 查询扩展通过本库提供的 clauses 使用。

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

## TDengine 查询扩展

本库提供以下 clauses：

- `CREATE TABLE` / `CREATE STABLE`
- `USING ... TAGS`
- `INTERVAL`、`SESSION`、`STATE_WINDOW`
- `FILL`
- `SLIMIT` / `SOFFSET`

完整示例见 [example/example.go](./example/example.go)。

## 集成测试

启动 TDengine 和 taosAdapter 后设置不包含数据库名的 WebSocket endpoint：

```powershell
$env:TDENGINE_GORM_TEST_ENDPOINT='root:taosdata@ws(127.0.0.1:6041)'
go test -v -count=1 -run 'Integration$' .
```

测试会创建临时数据库，并在结束时自动删除。GitHub Actions 也会使用 TDengine 3.4.1.6 执行同一套测试。

# 从 v0.2.0 升级

当前 TDengine 3.x 适配改造保持原模块路径不变：

```text
github.com/FEINIAO233/tdengine-gorm-ws
```

这里的“v2 方案”是库的第二版适配方案，不是 Go Modules 的 `v2.0.0`，因此导入路径不需要增加 `/v2`。

## 环境要求

- Go 最低版本从 1.13 调整为 1.18。
- GORM 从 1.25.x 升级到 1.31.x。
- 官方 TDengine Go 驱动保持为 `driver-go/v3` 3.8.2。
- 推荐 TDengine 3.3.6 或更高版本。

## 连接字符串

只使用 WebSocket DSN：

```text
root:taosdata@ws(127.0.0.1:6041)/database?timezone=Asia%2FShanghai
```

旧的 `tcp(6030)` 原生连接示例已经移除。TLS 连接使用 `wss(...)`。

## SQL 行为变化

- 字符串和 `[]byte` 参数会按照 TDengine 转义规则编码，修复标签和查询字符串未加引号的问题。
- 数据库、表、超级表、列和标签名会使用反引号引用，保留字和合法的特殊标识符可正常使用。
- `uint` 字段会映射到 TDengine unsigned 整数类型，不再错误映射为 signed 类型。
- `SetUsing` 的 map 标签按名称排序，生成结果稳定。
- 新增 `SetUsingTags`，可以显式控制标签列顺序。
- `ADDTagPair` 仍可使用，但已弃用，请改用 `AddTag`。
- 同一个 `CreateTable` clause 中的多个子表会使用 TDengine 批量建表语法。
- GORM slice 批量写入会生成 TDengine 3.x 的 `VALUES (...) (...)` 语法。
- 支持 `gorm.Config{PrepareStmt: true}`、`interpolateParams=false` 以及显式 `BindModePrepared`；预编译模式不再预先把字符串转换为 SQL 字面量。
- GORM `ON CONFLICT` clause 会明确返回 `ErrOnConflictUnsupported`，避免发送 TDengine 不支持的语法。

TDengine 3.3.x 应继续使用默认插值模式；Prepared/Stmt2 真实服务兼容基线为 TDengine 3.4+。

## 自动迁移

现在支持面向 TDengine 的安全增量 `AutoMigrate`：

- 没有 `tdengine:"tag"` 字段时创建普通表。
- 存在 `tdengine:"tag"` 字段时创建超级表。
- 已存在的表只新增缺失的普通列或标签。
- 不自动删除字段，也不自动修改字段类型。
- 第一个非标签字段必须是 `TIMESTAMP`。
- 可使用 `tdengine:"compositeKey"` 将一个整数或 `VARCHAR` 数据字段声明为附加复合主键；该定义只能在首次建表时使用。
- 虚拟表可以查询，但 Migrator 不会对其生成物理表 DDL，结构变更会返回 `ErrVirtualTableUnsupported`。

超级表标签定义通过 `DESCRIBE` 检测；`INFORMATION_SCHEMA.INS_TAGS` 只用于子表标签值，空超级表不会依赖该视图判断标签是否存在。

类型修改、删除和标签重命名必须通过 `tdengine.Migrator` 的显式方法执行并自行评估数据影响。

新增 `SetTableTags` 和 `SetTableTagsBatch`，分别用于单个子表多 Tag 修改和多个子表批量修改；后者需要 TDengine 3.4+。

底层建表 clause 支持 `COMMENT`、`TTL`、`SMA`、`KEEP` 以及字段级 `ENCODE`、`COMPRESS`、`LEVEL` 参数，并会校验不合法的参数组合。

新增超级表 Tag Index 迁移支持：

- `gorm:"index:index_name" tdengine:"tag"` 可由 `AutoMigrate` 创建。
- 支持 `CreateIndex`、`DropIndex`、`HasIndex` 和 `GetIndexes`。
- 普通列、多列、唯一和表达式索引会明确返回不支持错误。
- 约束和表重命名不再落入 GORM 通用 SQL，而是明确返回 TDengine 不支持错误。

新增 `VARCHAR`、`VARBINARY`、`GEOMETRY`、`DECIMAL`、`BLOB` 和 JSON Tag 的类型生成及迁移校验。

## 查询扩展

新增以下 TDengine clauses：

- `partition.Columns` / `partition.Expressions`
- `window.SetEventWindow`
- `window.SetCountWindow`
- `interp.SetRange` / `interp.SetEvery`

这些 clauses 会按照 `PARTITION BY -> RANGE/EVERY -> WINDOW -> FILL` 的 TDengine SQL 顺序生成。

## 更新与删除

- 普通 GORM `Update` / `Updates` 现在明确返回 `ErrUpdateNotSupported`。TDengine 的更新语义是重新插入相同时间戳。
- 普通 GORM `Delete` 明确返回 `ErrDeleteNotSupported`。
- 新增 `DeleteTimeRange`，只允许至少带一个时间边界的删除，范围为开始时间包含、结束时间不包含。

## 测试

普通 `go test ./...` 不要求本地 TDengine。真实服务测试需要设置：

```text
TDENGINE_GORM_TEST_ENDPOINT=root:taosdata@ws(127.0.0.1:6041)
```

集成测试会自行创建和清理临时数据库。

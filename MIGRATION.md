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

## 测试

普通 `go test ./...` 不要求本地 TDengine。真实服务测试需要设置：

```text
TDENGINE_GORM_TEST_ENDPOINT=root:taosdata@ws(127.0.0.1:6041)
```

集成测试会自行创建和清理临时数据库。

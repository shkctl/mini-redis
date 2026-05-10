# mini-redis 测试规则

## 测试初始化模板
每个测试文件需要 setup 函数，调用顺序必须严格一致：
```go
func setupXxxTest() {
    createSharedObjects()   // 1. 共享对象（必须在最前面）
    loadServerConfig()      // 2. 默认配置
    initServerConfig()      // 3. 配置初始化
    initServer()            // 4. 服务器初始化
    // 5. 模块特定初始化（显式设置，确保 TDD 早期阶段可工作）
}
```

## 测试分类命名规范
- `TestXxx_Basic`：基本正向功能验证
- `TestXxx_EdgeCase`：边界条件（空值、极值、溢出）
- `TestXxx_Disabled`：功能禁用时的行为（负数阈值等）
- `TestXxx_Threshold`：阈值/配置过滤行为
- `TestRedisCompat_Xxx`：与 Redis 行为兼容性验证
- `TestIntegration_Xxx`：多函数协作的端到端流程
- `TestConfigXxx`：配置加载和解析相关

## 辅助函数
```go
// 创建测试用 robj 参数数组
func createTestArgs(args ...string) []*robj {
    result := make([]*robj, len(args))
    for i, arg := range args {
        s := arg
        result[i] = createStringObject(&s, len(s))
    }
    return result
}
```

## 链表数据提取
```go
// 从链表节点提取 slowlogEntry（适配其他类型时替换类型）
node := listFirst(server.slowlog)
entry := (*node.value).(*slowlogEntry)
```

## 断言模式
- 使用标准库 testing，不引入第三方断言库
- 配置验证：直接比较 server 字段值
- 顺序验证：遍历链表/数组，逐个断言
- 集合验证：用 map 去重检查唯一性（如 ID 唯一性）
- 时间验证：记录 before/after 时间戳，验证 entry.time 在范围内

## 测试运行命令
```bash
# 按模块运行
go test -v -run TestSlowlog

# 按功能运行
go test -v -run TestSlowlogPushEntry
go test -v -run TestRedisCompat

# 带覆盖率
go test -v -cover -run Slowlog

# 生成覆盖率报告
go test -coverprofile=coverage.out -run Slowlog
go tool cover -html=coverage.out
```

## TDD 开发节奏
1. 写测试 → 运行确认失败 → 写最小实现 → 运行确认通过
2. 每个增量只加一个测试维度（基本→边界→禁用→兼容性）
3. 不要跳过"运行确认失败"步骤——验证测试本身有效
---
name: redis-command-scaffold
description: Redis 命令/功能脚手架，用于在 mini-redis 中实现新的 Redis 功能时提供标准化开发流程
---

# Redis 命令脚手架

## 适用场景
当用户要求在 mini-redis 中实现新的 Redis 命令或功能模块时使用。

## 开发步骤

### Phase 1: 源码分析
1. 定位 Redis C 源码对应文件（参考 memory: redis-source-reference）
2. 提取关键信息：
   - 数据结构定义（struct/typedef）
   - 常量和宏定义
   - 核心函数签名和逻辑
   - 配置参数

### Phase 2: 结构扩展
3. 在 `redis.go` const 块添加常量（与 Redis 宏定义对应）
4. 在 `redisServer` 结构体添加字段
5. 在 `loadServerConfig()` 设置默认值
6. 在 `initServer()` 中调用初始化函数

### Phase 3: 核心实现
7. 创建新文件 `xxx.go`，定义：
   - 结构体（对应 Redis struct）
   - 初始化函数（对应 Redis xxxInit()）
   - 核心逻辑函数（对应 Redis xxxPush/xxxGet 等）
   - 命令处理函数（对应 Redis xxxCommand()）

### Phase 4: 命令注册
8. 在 `command.go` 的 `redisCommandTable` 添加条目：
```go
{name: "XXX", proc: xxxCommand, arity: N, sflag: "wmF", flag: 0}
```

### Phase 5: 测试（TDD）
9. 创建 `xxx_test.go`，按模板组织：
   - setup 函数
   - 基本功能测试
   - 边界条件测试
   - 禁用/异常行为测试
   - Redis 兼容性测试
   - 集成测试

### Phase 6: 验证
10. 运行 `go test -v -run Xxx`
11. 运行 `go build` 确认编译通过
12. 与 Redis 真实行为对比验证

## 检查清单
- [ ] 常量值与 Redis 源码一致
- [ ] 函数命名与 Redis 源码对应
- [ ] 错误消息与 Redis 原文一致
- [ ] 负数阈值禁用、零阈值全记录
- [ ] ID/计数器重置后继续递增
- [ ] FIFO 队列行为正确
- [ ] 参数截断规则实现
- [ ] 测试覆盖基本/边界/禁用/兼容性场景
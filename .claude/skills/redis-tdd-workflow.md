---
name: redis-tdd-workflow
description: mini-redis 项目的 TDD 开发工作流，定义红-绿-重构的节奏和测试组织方式
---

# mini-redis TDD 工作流

## 适用场景
在 mini-redis 项目中用 TDD 方式开发新功能时使用。

## 核心节奏：红 → 绿 → 增量

### Step 1: 写测试（红）
```bash
# 1. 写一个失败的测试
# 2. 运行确认失败（验证测试本身有效）
go test -v -run TestXxx_Basic
# 预期：编译错误或测试失败
```

### Step 2: 写最小实现（绿）
```bash
# 1. 写刚好让测试通过的代码
# 2. 运行确认通过
go test -v -run TestXxx_Basic
# 预期：PASS
```

### Step 3: 增量扩展
按以下顺序逐个添加测试维度，每个维度重复 红→绿：

1. **基本功能** → `_Basic`：正向路径验证
2. **边界条件** → `_EdgeCase`：空值、极值、溢出
3. **禁用行为** → `_Disabled`：负数阈值、功能关闭
4. **阈值过滤** → `_Threshold`：配置变更后的行为
5. **Redis 兼容性** → `TestRedisCompat_`：与 Redis 行为对齐
6. **集成流程** → `TestIntegration_`：多函数协作

### Step 4: 重构（可选）
当所有测试通过后，检查是否需要：
- 提取公共逻辑
- 消除重复代码
- 但不要过度抽象

## 测试文件组织模板
```go
package main

import (
    "testing"
)

// === Setup ===
func setupXxxTest() {
    createSharedObjects()
    loadServerConfig()
    initServerConfig()
    initServer()
    // 模块特定初始化
}

// === 基本功能 ===
func TestXxx_Basic(t *testing.T) { ... }

// === 边界条件 ===
func TestXxx_EdgeCase(t *testing.T) { ... }

// === 禁用行为 ===
func TestXxx_Disabled(t *testing.T) { ... }

// === Redis 兼容性 ===
func TestRedisCompat_Xxx(t *testing.T) { ... }

// === 集成测试 ===
func TestIntegration_Xxx(t *testing.T) { ... }
```

## 关键原则
- 每次只加一个测试维度
- 不要跳过"运行确认失败"步骤
- 不要在测试中引入外部依赖
- 使用标准库 testing 包
- setup 函数在每个测试中独立调用（不用 TestMain）
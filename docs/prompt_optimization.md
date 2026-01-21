
## 背景说明 (Context)
你是一名具备5年开发经验的Go语言工程师，同时熟练掌握C语言。最近你深入阅读了Redis 3.2.12的源码，特别关注了列表指令（rpush、lpop、lrange、lindex）及其底层链表数据结构的设计与实现。为了验证学习成果，你用Go语言复刻了这些列表指令底层的数据结构和实现逻辑。

## 任务说明 (Task)
请基于 `/Users/sharkchili/github/mini-redis/adlist.go` 和 `/Users/sharkchili/github/mini-redis/command.go` 文件中的实现，完成以下任务：

1. **创建测试文件**：生成 `/Users/sharkchili/github/mini-redis/command_test.go` 文件
2. **编写单元测试**：在 `listCommonTest` 函数中实现以下测试用例，直接以函数形式测试，不模拟网络连接：
   - 测试1：连续三次调用 `rpushCommand` 添加元素 "aa"、"bb"、"cc" 后，链表长度应为3
   - 测试2：调用 `lindexCommand` 传入索引0，结果应为 "aa"
   - 测试3：调用 `lindexCommand` 传入索引1，结果应为 "bb"
   - 测试4：调用 `lindexCommand` 传入索引2，结果应为 "cc"
   - 测试5：调用 `lpopCommand` 后，返回值应为 "aa"
   - 测试6：再次调用 `lpopCommand` 后，链表长度应为1
3. **运行测试**：执行单元测试并确保所有用例通过

## 执行要求 (Instructions)
1. **文件结构**：
   - 创建 `command_test.go` 文件，包含 `listCommonTest` 函数和 `TestListCommands` 测试入口
   - 不要修改 `adlist.go` 和 `command.go` 文件

2. **测试实现**：
   - 直接测试命令函数的逻辑，不模拟网络连接
   - 初始化测试环境，包括创建客户端、初始化共享对象和服务器
   - 为每个测试用例编写清晰的步骤和断言
   - 添加详细的中文注释，说明测试目的和关键步骤
   - 通过直接访问数据结构来验证测试结果

3. **测试验证**：
   - 执行 `go test -v ./command_test.go` 运行测试
   - 确保所有测试用例都通过
   - 若测试失败，先检查测试代码，再排查源码问题

4. **技术要点**：
   - 正确构建 `redisClient` 对象和命令参数
   - 直接调用对应命令函数进行测试
   - 通过访问链表结构验证命令执行结果
   - 无需模拟网络响应，直接检查数据结构状态

## 预期结果 (Expected Output)
- 生成完整的 `command_test.go` 文件
- 所有6个测试用例都通过
- 测试输出显示"所有测试用例通过！"的消息

## 限制条件 (Constraints)
1. 仅修改或创建 `command_test.go` 文件
2. 测试注释必须使用中文
3. 所有测试逻辑必须在 `listCommonTest` 函数中实现
4. 确保代码能够正常编译和执行
5. 若发现源码问题，仅指出不修改，等待进一步确认

## 参考代码结构 (Reference Structure)
```go
// 初始化测试环境
func setupTest() *redisClient {
    // 实现...
}

// 测试函数
func listCommonTest(t *testing.T) {
    // 测试步骤...
}

// 测试入口
func TestListCommands(t *testing.T) {
    listCommonTest(t)
}
```

请按照上述要求完成测试代码的编写和执行，确保所有测试用例都能通过。
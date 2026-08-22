# OShinC

Go + Lua 脚本执行引擎，提供安全沙箱环境和双向权限控制。

## 特性

- **安全沙箱**：AST 静态检测 + 运行时权限控制
- **双向授权**：Lua 申请 → 内核监控 → 前端（人类）批准 → 执行
- **自定义函数**：前端注册，Lua 队列调用，等待前端返回结果
- **多调用方式**：Go API、CLI 命令行、FFI 共享库（Python/Node/C）
- **三种执行模式**：direct（直接调用）、route（路由）、pipeline（管道）

## 快速开始

### Go API

```go
import "oshin-core/plugin"

core := plugin.NewCore()
resp := core.Execute(plugin.PluginRequest{
    Script: `function main(params) return {sum = params.a + params.b} end`,
    Params: map[string]interface{}{"a": 10, "b": 20},
    Mode:   "direct",
})
// resp.Data = map[string]interface{}{"sum": float64(30)}
```

### CLI

```bash
# 编译
set CGO_ENABLED=1
go build -o oshin-cli.exe ./cmd/cli/

# 执行脚本
oshin-cli.exe test/simple.lua

# JSON 模式
echo '{"script":"function main(p) return {ok=true} end"}' | oshin-cli.exe --json
```

### FFI 共享库

```bash
# 编译 DLL
set CGO_ENABLED=1
go build -buildmode=c-shared -o oshin.dll ./cmd/ffi/
```

```python
from oshin_core import OShinCore

oshin = OShinCore(lib_path="oshin.dll", permission_callback=my_perm)
result = oshin.execute_direct('function main(p) return {ok=true} end', {})
```

## 架构

```
plugin/
  core.go      - 执行引擎 (Lua 脚本执行、内置函数注册)
  sandbox.go   - 安全沙箱 (权限模型、脚本验证、环境隔离)
cmd/
  cli/         - 命令行工具 (直接调用 plugin 包)
  ffi/         - FFI 共享库 (cgo 导出 C 接口，供 Python/Node 等调用)
test/
  oshin_core.py        - Python 调用封装 (ctypes)
  example.py           - Python 调用示例
  test_permission.py   - 权限系统测试
  test.lua             - Lua 功能测试脚本
```

## 文档

详细文档请参阅 [docs.md](docs.md)，包含：

- [Go API 完整参考](docs.md#go-api)
- [FFI 共享库接口](docs.md#ffi-共享库接口)
- [自定义函数](docs.md#自定义函数)
- [CLI 命令行工具](docs.md#cli-命令行工具)
- [权限系统详解](docs.md#权限系统)
- [Lua 函数参考](docs.md#lua-函数参考)
- [Python 绑定](docs.md#python-绑定)

## 许可证

MIT License

# OShinC 内核文档

## 目录

- [Go API](#go-api)
- [FFI 共享库接口](#ffi-共享库接口)
- [自定义函数](#自定义函数)
- [CLI 命令行工具](#cli-命令行工具)
- [权限系统](#权限系统)
- [Lua 函数参考](#lua-函数参考)
- [Python 绑定](#python-绑定)

---

## Go API

### 基本使用

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

### 带权限控制

```go
config := plugin.DefaultSecurityConfig()
config.PermissionCallback = func(req plugin.PermissionRequest) bool {
    fmt.Printf("请求权限: %s (%s)\n", req.Type, req.Description)
    return true // 允许所有
}

core := plugin.NewCoreWithConfig(config)
resp := core.Execute(req)
```

### 快捷函数

```go
// 直接执行脚本
result, err := plugin.ExecuteScript(script, params)

// 带配置执行
result, err := plugin.ExecuteScriptWithConfig(script, params, config)

// 路由模式
result, err := plugin.ExecuteRoute(script, action, params)

// 管道模式
result, err := plugin.ExecutePipeline(script, params)
```

### PluginRequest 结构

```go
type PluginRequest struct {
    Method  string                 `json:"method"`   // 函数名（direct 模式）
    Action  string                 `json:"action"`   // 动作名（route 模式）
    Params  map[string]interface{} `json:"params"`   // 参数
    Script  string                 `json:"script"`   // Lua 脚本
    Timeout int                    `json:"timeout"`  // 超时（毫秒）
    Mode    string                 `json:"mode"`     // 执行模式
}
```

### PluginResponse 结构

```go
type PluginResponse struct {
    Code    int         `json:"code"`    // 0=成功，其他=错误
    Message string      `json:"message"` // 错误信息
    Data    interface{} `json:"data"`    // 返回数据
    Time    int64       `json:"time"`    // 执行时间（毫秒）
}
```

### SecurityConfig 配置

```go
type SecurityConfig struct {
    Timeout            int                  // 超时时间（毫秒），默认 5000
    MaxMemoryMB        int                  // 最大内存（MB），默认 64
    PermissionCallback PermissionCallback   // 权限回调函数
}
```

### PermissionRequest 结构

```go
type PermissionRequest struct {
    Type        PermissionType    `json:"type"`        // 权限类型
    Description string            `json:"description"` // 描述
    Details     map[string]string `json:"details"`     // 附加信息
}
```

### 权限类型

```go
const (
    PermExec      PermissionType = "exec"       // 执行外部程序
    PermFileRead  PermissionType = "file_read"  // 读取文件
    PermFileWrite PermissionType = "file_write" // 写入文件
    PermNetwork   PermissionType = "network"    // 网络访问
    PermSystem    PermissionType = "system"     // 系统操作
)
```

---

## FFI 共享库接口

### 编译 DLL

```bash
set CGO_ENABLED=1
go build -buildmode=c-shared -o oshin.dll ./cmd/ffi/
```

### 导出函数

| 函数 | 说明 |
|---|---|
| `OShinExecute(script, params_json, mode, config_json)` | 执行脚本 |
| `OShinSetPermissionCallback(callback)` | 注册权限回调 |
| `OShinFreeString(str)` | 释放返回字符串 |
| `OShinVersion()` | 版本号 |

### OShinExecute

```c
char* OShinExecute(
    const char* script,        // Lua 脚本内容
    const char* params_json,   // 参数 JSON（可为 NULL）
    const char* mode,          // "direct" / "route:action" / "pipeline"
    const char* config_json    // 配置 JSON（可为 NULL）
);
```

**返回值**：JSON 字符串，需调用 `OShinFreeString()` 释放。

**配置 JSON 格式**：
```json
{
  "timeout": 5000,
  "max_memory_mb": 64
}
```

### OShinSetPermissionCallback

```c
void OShinSetPermissionCallback(void* callback);
```

**回调函数签名**：
```c
int callback(
    const char* perm_type,      // 权限类型
    const char* description,    // 描述
    const char* details_json    // 附加信息 JSON
);
// 返回 1=允许, 0=拒绝
```

### 使用示例（C）

```c
#include <stdio.h>
#include <stdlib.h>

// 声明导出函数
extern char* OShinExecute(const char*, const char*, const char*, const char*);
extern void OShinSetPermissionCallback(void*);
extern void OShinFreeString(char*);
extern char* OShinVersion();

// 权限回调
int my_perm_callback(const char* type, const char* desc, const char* details) {
    printf("权限请求: %s (%s)\n", type, desc);
    return 1; // 允许
}

int main() {
    // 注册回调
    OShinSetPermissionCallback(my_perm_callback);

    // 执行脚本
    char* result = OShinExecute(
        "function main(p) return {greeting='Hello ' .. p.name} end",
        "{\"name\":\"World\"}",
        "direct",
        NULL
    );

    printf("Result: %s\n", result);
    OShinFreeString(result);
    return 0;
}
```

---

## 自定义函数

支持前端向内核注册自定义函数，Lua 脚本可直接调用。调用请求以**队列方式**发送给前端，前端处理完毕后返回结果，Lua 调用阻塞等待。

### 工作流程

```
Lua 调用 send_email(...)
    ↓
内核构造请求 {"id":"...","name":"send_email","args":[...]} → 入队
    ↓
前端循环 OShinWaitCustomFunction() 取出请求 → 处理
    ↓
前端 OShinReturnCustomFunctionResult(id, result_json) 返回
    ↓
内核唤醒等待中的 Lua 调用，返回结果
```

### FFI 接口

| 函数 | 说明 |
|---|---|
| `OShinRegisterCustomFunction(name, arg_count)` | 注册自定义函数 |
| `OShinWaitCustomFunction(timeout_ms)` | 阻塞等待取请求（返回 JSON 或 NULL） |
| `OShinReturnCustomFunctionResult(id, result_json, error_msg)` | 返回结果 |

### Go 接口

```go
core := plugin.NewCore()

// 注册自定义函数
core.RegisterCustomFunction("send_email", 2)

// 前端消费循环（独立 goroutine/线程）
go func() {
    for {
        req, ok := core.WaitCustomFunction(10 * time.Second)
        if !ok {
            break
        }
        // 处理请求...
        core.ReturnCustomFunctionResult(req.ID, result, "")
    }
}()
```

### Lua 调用约定

```lua
-- 成功时返回单个结果值
local result = send_email("alice@example.com", "hello")
-- result = {status="sent", to="alice@example.com"}

-- 失败时返回 nil + 错误信息
local result, err = send_email("bad@example.com", "hello")
-- result = nil, err = "custom function send_email: <错误信息>"
```

### 请求 JSON 格式

```json
{
  "id": "a1b2c3d4",
  "name": "send_email",
  "args": ["alice@example.com", "hello"]
}
```

### 结果 JSON 格式

```json
{"status": "sent", "to": "alice@example.com"}
```

**注意**：前端必须及时调用 `OShinReturnCustomFunctionResult` 返回结果，Lua 调用默认等待 60 秒，超时返回错误。

---

## CLI 命令行工具

### 编译

```bash
set CGO_ENABLED=1
go build -o oshin-cli.exe ./cmd/cli/
```

### 直接执行模式

```bash
# 直接执行 Lua 脚本
oshin-cli.exe test/simple.lua

# 指定模式和动作
oshin-cli.exe test/simple.lua route add
oshin-cli.exe test/simple.lua pipeline

# 传递参数
oshin-cli.exe test/simple.lua direct main '{"a":10,"b":20}'
```

**输出格式**：JSON
```json
{"code":0, "message":"success", "data":{...}, "time":123}
```

### 交互模式

```bash
oshin-cli.exe

# 命令列表
oshin-cli> help

# 执行脚本 (直接模式)
oshin-cli> exec test/simple.lua

# 执行脚本 (路由模式)
oshin-cli> exec test/simple.lua route add

# 执行脚本 (管道模式)
oshin-cli> exec test/simple.lua pipeline

# 退出
oshin-cli> exit
```

### JSON 模式

通过 `--json` 标志启用：从 stdin 读取请求 JSON，结果输出到 stdout。Lua 的 `log()` 输出到 stderr。

**请求格式**：
```json
{
  "script": "...",
  "script_file": "path/to/script.lua",
  "mode": "direct",
  "action": "",
  "params": {},
  "timeout": 5000
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
|---|---|---|
| `script` | string | Lua 脚本内容（与 `script_file` 二选一） |
| `script_file` | string | Lua 脚本文件路径（优先级低） |
| `mode` | string | `direct`（默认）/ `route` / `pipeline` |
| `action` | string | 路由模式下的动作名称 |
| `params` | object | 传递给脚本的参数 |
| `timeout` | int | 超时时间（毫秒） |

**示例**：

```bash
# 基本调用
echo '{"script":"function main(p) return {greeting=\"Hello \" .. p.name} end", "params":{"name":"World"}}' | oshin-cli.exe --json

# 从文件加载脚本
echo '{"script_file":"test/simple.lua", "params":{"name":"test"}}' | oshin-cli.exe --json

# 路由模式
echo '{"script_file":"test/simple.lua", "mode":"route", "action":"add", "params":{"a":10,"b":20}}' | oshin-cli.exe --json

# 管道模式
echo '{"script_file":"test/simple.lua", "mode":"pipeline", "params":{"value":42}}' | oshin-cli.exe --json
```

**Shell 调用示例**：

```bash
# PowerShell
$json = '{"script":"function main(p) return {ok=true} end"}'
$json | oshin-cli.exe --json | ConvertFrom-Json

# Bash
result=$(echo '{"script":"function main(p) return {ok=true} end"}' | ./oshin-cli.exe --json)
echo $result
```

### CLI vs FFI 对比

| 特性 | CLI 直接模式 | CLI JSON 模式 | FFI 共享库 |
|---|---|---|---|
| 调用方式 | 命令行参数 | 子进程 + stdin/stdout | 函数调用 |
| 权限回调 | 无（默认拒绝） | 无（默认拒绝） | 支持 C 回调 |
| 性能 | 较低（进程开销） | 较低（进程开销） | 高（无进程开销） |
| 适用场景 | 开发调试、简单脚本 | 脚本编排、CI/CD | 应用内嵌入、高性能场景 |

### CLI 安全模型

- 脚本通过 `request_permission(type, description)` 主动请求权限
- CLI 模式下默认拒绝所有敏感操作（无权限回调）
- 脚本应在执行敏感操作前先调用 `request_permission()` 检查权限
- 权限类型：`exec`, `network`, `file_read`, `file_write`, `system`
- `system` 权限控制 os 包危险函数：`execute`, `exit`, `getenv`, `remove`, `rename`, `tmpname`

---

## 权限系统

### 双向授权流程

```
Lua 调用敏感函数（如 http_request）
    ↓
内核调用 RequestPermission()
    ↓
内核调用 PermissionCallback
    ↓
回调通过 C 桥接层调用前端函数
    ↓
前端询问用户（或自动响应）
    ↓
前端返回 bool（1=允许, 0=拒绝）
    ↓
内核根据返回值决定是否执行
```

### 权限类型

| 权限类型 | 说明 | 管控函数 |
|---|---|---|
| `exec` | 执行外部程序 (Python/Node/Lua 等) | `execute_external()` |
| `network` | 网络访问 (HTTP 请求) | `http_request()` |
| `file_read` | 读取本地文件 | `read_file()` |
| `file_write` | 写入本地文件 | `write_file()` |
| `system` | 系统操作 (os包危险函数) | `os.execute()`, `os.exit()`, `os.getenv()`, `os.remove()`, `os.rename()`, `os.tmpname()` |

### 安全函数（无需权限）

- `os.time()`, `os.date()`, `os.difftime()`, `os.clock()`, `os.setlocale()`

### Lua 中使用权限

```lua
-- 主动请求权限
local ok = request_permission("network", "访问外部API")
if ok then
    local data = http_request("https://api.example.com/data")
    -- ...
end

-- 使用 os 包安全函数（无需权限）
local now = os.time()

-- 使用 os 包危险函数（需要 system 权限）
local ok = request_permission("system", "执行系统命令")
if ok then
    os.execute("ls -la")
end
```

### 静态检测（AST 解析）

脚本加载前通过 AST 解析检测危险函数调用：

- 拒绝包含 `load()`, `dofile()`, `loadfile()`, `loadstring()` 等危险函数的脚本
- 拒绝访问 `debug`, `io`, `package` 等危险全局变量
- 拒绝通过 `_G["load"]` 或 `_G.load` 等方式绕过检测

---

## Lua 函数参考

### 基础函数

| 函数 | 权限要求 | 说明 |
|---|---|---|
| `request_permission(type, desc)` | 无 | 请求权限 (返回 true/false) |
| `http_request(url, method, body)` | `network` | HTTP 请求 |
| `execute_external(program, code)` | `exec` | 执行外部程序 |
| `read_file(path)` | `file_read` | 读取文件 |
| `write_file(path, content)` | `file_write` | 写入文件 |
| `json_parse(str)` | 无 | JSON 解析 |
| `json_stringify(val)` | 无 | JSON 序列化 |
| `log(msg)` | 无 | 日志输出 |

### 编码 / 哈希函数

| 函数 | 权限要求 | 说明 |
|---|---|---|
| `url_encode(str)` | 无 | URL 编码 |
| `url_decode(str)` | 无 | URL 解码 |
| `md5(str)` | 无 | MD5 哈希（十六进制） |
| `sha1(str)` | 无 | SHA-1 哈希（十六进制） |
| `sha256(str)` | 无 | SHA-256 哈希（十六进制） |
| `base64_encode(str)` | 无 | Base64 编码 |
| `base64_decode(str)` | 无 | Base64 解码 |
| `hex_encode(str)` | 无 | Hex 编码 |
| `hex_decode(str)` | 无 | Hex 解码 |

### os 包函数

| 函数 | 权限要求 | 说明 |
|---|---|---|
| `os.time()`, `os.date()`, `os.difftime()`, `os.clock()`, `os.setlocale()` | 无 | 安全函数，可直接使用 |
| `os.execute(cmd)` | `system` | 执行系统命令 |
| `os.exit([code])` | `system` | 退出程序 |
| `os.getenv(varname)` | `system` | 获取环境变量 |
| `os.remove(filename)` | `system` | 删除文件 |
| `os.rename(oldname, newname)` | `system` | 重命名文件 |
| `os.tmpname()` | `system` | 生成临时文件名 |

---

## Python 绑定

### 基本使用

```python
from oshin_core import OShinCore

# 带权限回调
def my_perm(perm_type, description, details):
    print(f"请求: {description}")
    return input("允许? (y/n): ").lower() == "y"

oshin = OShinCore(lib_path="oshin.dll", permission_callback=my_perm)

# 直接执行
r = oshin.execute_direct(
    'function main(params) return {greeting = "Hello, " .. params.name .. "!"} end',
    {"name": "World"},
)

# 路由模式
r = oshin.execute_route(script, "action_name", params)
```

### 运行示例

```bash
python test/example.py
python test/test_permission.py
```

---

## 执行模式

| 模式 | 说明 | 入口 |
|---|---|---|
| `direct` | 直接调用 `main(params)` 或指定函数 | `PluginRequest.Method` |
| `route` | 通过 `routes` 表按 action 路由 | `PluginRequest.Action` |
| `pipeline` | 执行 `pipeline(params)` 管道 | 固定入口 |

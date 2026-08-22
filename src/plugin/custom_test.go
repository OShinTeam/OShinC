package plugin

import (
	"testing"
	"time"
)

// 正常流程：注册 + 前端消费 + 返回结果
func TestCustomFunctionNormalFlow(t *testing.T) {
	core := NewCore()
	core.RegisterCustomFunction("send_email", 2)

	go func() {
		req, ok := core.WaitCustomFunction(5 * time.Second)
		if !ok {
			return
		}
		core.ReturnCustomFunctionResult(req.ID, map[string]interface{}{"status": "sent"}, "")
	}()

	resp := core.Execute(PluginRequest{
		Script: `function main() return send_email("a@b.com", "hi") end`,
		Mode:   "direct",
		Method: "main",
	})
	if resp.Code != 0 {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Message)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok || data["status"] != "sent" {
		t.Fatalf("unexpected result: %v", resp.Data)
	}
}

// 参数个数不匹配：注册 argCount=2，调用传 3 个参数 → Lua 侧应收到错误
func TestCustomFunctionArgCountMismatch(t *testing.T) {
	core := NewCore()
	core.RegisterCustomFunction("calc", 2)

	resp := core.Execute(PluginRequest{
		Script: `
function main()
    local ok, err = calc(1, 2, 3)
    if not ok then return {error = err} end
    return {value = ok}
end`,
		Mode:   "direct",
		Method: "main",
	})
	if resp.Code != 0 {
		t.Fatalf("unexpected core error: %s", resp.Message)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result: %v", resp.Data)
	}
	if _, hasErr := data["error"]; !hasErr {
		t.Fatalf("expected arg count error in lua, got: %v", resp.Data)
	}
}

// 队列满超时保护：填满队列后调用不应永久阻塞
func TestCustomFunctionQueueFullTimeout(t *testing.T) {
	// 临时缩短超时，避免测试等待 60s
	old := customCallTimeout
	customCallTimeout = 500 * time.Millisecond
	defer func() { customCallTimeout = old }()

	reg := NewCustomFunctionRegistry()
	reg.Register("slow_fn", 0)

	// 填满队列（128 条）
	for i := 0; i < customQueueSize; i++ {
		select {
		case reg.queue <- CustomFunctionRequest{ID: "x", Name: "slow_fn"}:
		default:
			t.Fatalf("queue should have capacity %d", customQueueSize)
		}
	}

	// 第 129 次调用：入队应超时返回错误（不永久阻塞）
	done := make(chan struct{})
	var err error
	go func() {
		_, err = reg.Call("slow_fn", nil)
		close(done)
	}()

	select {
	case <-done:
		if err == nil {
			t.Fatal("expected timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Call blocked forever on full queue")
	}
}

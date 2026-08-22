package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// CustomFunctionRequest 自定义函数调用请求（入队发送给前端）
type CustomFunctionRequest struct {
	ID   string        `json:"id"`
	Name string        `json:"name"`
	Args []interface{} `json:"args"`
}

// CustomFunctionResult 前端返回的调用结果
type CustomFunctionResult struct {
	ID    string      `json:"id"`
	Value interface{} `json:"value"`
	Error string      `json:"error"`
}

// CustomFunctionRegistry 自定义函数注册表 + 调用队列
// 前端注册函数名后，Lua 调用时内核将请求入队，等待前端返回结果
type CustomFunctionRegistry struct {
	mu      sync.Mutex
	fns     map[string]int                          // 函数名 -> 参数个数
	queue   chan CustomFunctionRequest              // 待前端处理的请求队列
	pending map[string]chan CustomFunctionResult    // 请求ID -> 结果通道
}

const customQueueSize = 128

// customCallTimeout 自定义函数调用超时（var 便于测试覆盖）
var customCallTimeout = 60 * time.Second

// NewCustomFunctionRegistry 创建注册表
func NewCustomFunctionRegistry() *CustomFunctionRegistry {
	return &CustomFunctionRegistry{
		fns:     make(map[string]int),
		queue:   make(chan CustomFunctionRequest, customQueueSize),
		pending: make(map[string]chan CustomFunctionResult),
	}
}

// Register 注册自定义函数（argCount: 期望参数个数）
func (r *CustomFunctionRegistry) Register(name string, argCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fns[name] = argCount
}

// Names 返回全部已注册函数名
func (r *CustomFunctionRegistry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.fns))
	for name := range r.fns {
		names = append(names, name)
	}
	return names
}

// ArgCount 返回函数期望参数个数（未注册返回 -1）
func (r *CustomFunctionRegistry) ArgCount(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.fns[name]; ok {
		return n
	}
	return -1
}

// Call 调用自定义函数：请求入队并阻塞等待前端返回结果
func (r *CustomFunctionRegistry) Call(name string, args []interface{}) (interface{}, error) {
	req := CustomFunctionRequest{
		ID:   newRequestID(),
		Name: name,
		Args: args,
	}

	resultCh := make(chan CustomFunctionResult, 1)
	r.mu.Lock()
	r.pending[req.ID] = resultCh
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.pending, req.ID)
		r.mu.Unlock()
	}()

	timer := time.NewTimer(customCallTimeout)
	defer timer.Stop()

	// 入队带超时保护：队列满且前端不消费时，避免永久阻塞
	select {
	case r.queue <- req:
	case <-timer.C:
		return nil, &CustomFunctionError{Name: name, Err: "queue full: frontend not consuming requests"}
	}

	// 等待前端返回结果
	select {
	case res := <-resultCh:
		if res.Error != "" {
			return nil, &CustomFunctionError{Name: name, Err: res.Error}
		}
		return res.Value, nil
	case <-timer.C:
		return nil, &CustomFunctionError{Name: name, Err: "timeout waiting for frontend result"}
	}
}

// WaitRequest 阻塞等待前端取请求（带超时）
// 返回请求及是否取到
func (r *CustomFunctionRegistry) WaitRequest(timeout time.Duration) (CustomFunctionRequest, bool) {
	select {
	case req := <-r.queue:
		return req, true
	case <-time.After(timeout):
		return CustomFunctionRequest{}, false
	}
}

// ReturnResult 前端返回结果，唤醒等待中的调用
func (r *CustomFunctionRegistry) ReturnResult(id string, value interface{}, errMsg string) bool {
	r.mu.Lock()
	ch, ok := r.pending[id]
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- CustomFunctionResult{ID: id, Value: value, Error: errMsg}
	return true
}

// CustomFunctionError 自定义函数调用错误
type CustomFunctionError struct {
	Name string
	Err  string
}

func (e *CustomFunctionError) Error() string {
	return "custom function " + e.Name + ": " + e.Err
}

func newRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

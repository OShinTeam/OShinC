package main

/*
#include <stdlib.h>

// 宿主程序的权限回调函数类型
// perm_type:     "exec", "network", "file_read", "file_write", "system"
// description:   可读的权限描述
// details_json:  附加信息 JSON (url, path, program 等)
// 返回 1=允许, 0=拒绝
typedef int (*oshin_perm_cb)(const char* perm_type, const char* description, const char* details_json);

static oshin_perm_cb g_perm_callback = NULL;

static void oshin_set_perm_callback(oshin_perm_cb cb) {
    g_perm_callback = cb;
}

static int oshin_check_perm(const char* perm_type, const char* description, const char* details_json) {
    if (g_perm_callback == NULL) return 0;
    return g_perm_callback(perm_type, description, details_json);
}
*/
import "C"

import (
	"encoding/json"
	"strings"
	"time"
	"unsafe"

	"oshin-core/plugin"
)

var version = "1.0.0" // 版本号，可通过 -ldflags "-X main.version=xxx" 注入

// 全局自定义函数注册表：跨 OShinExecute 调用保持注册
var globalCustomRegistry = plugin.NewCustomFunctionRegistry()

// 设置权限回调函数。宿主程序必须在首次 Execute 之前调用。
// callback 签名: int callback(perm_type, description, details_json)
// 返回 1=允许, 0=拒绝
//
//export OShinSetPermissionCallback
func OShinSetPermissionCallback(cCallback unsafe.Pointer) {
	C.oshin_set_perm_callback(C.oshin_perm_cb(cCallback))
}

// mode 格式: "direct", "route:action_name", "pipeline"
// configJSON 格式: {"timeout":5000,"max_memory_mb":64}
//
//export OShinExecute
func OShinExecute(cScript *C.char, cParams *C.char, cMode *C.char, cConfigJSON *C.char) *C.char {
	script := C.GoString(cScript)
	modeRaw := C.GoString(cMode)

	var params map[string]interface{}
	if cParams != nil {
		paramsJSON := C.GoString(cParams)
		if paramsJSON != "" {
			if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
				resp := plugin.PluginResponse{Code: 1, Message: "Invalid params: " + err.Error()}
				data, _ := json.Marshal(resp)
				return C.CString(string(data))
			}
		}
	}
	if params == nil {
		params = make(map[string]interface{})
	}

	// 解析 configJSON
	config := plugin.DefaultSecurityConfig()
	if cConfigJSON != nil {
		configJSON := C.GoString(cConfigJSON)
		if configJSON != "" {
			var cfgMap map[string]interface{}
			if err := json.Unmarshal([]byte(configJSON), &cfgMap); err == nil {
				if v, ok := cfgMap["timeout"].(float64); ok {
					config.Timeout = int(v)
				}
				if v, ok := cfgMap["max_memory_mb"].(float64); ok {
					config.MaxMemoryMB = int(v)
				}
			}
		}
	}

	// 设置权限回调：通过 C 层桥接到宿主程序
	config.PermissionCallback = func(req plugin.PermissionRequest) bool {
		detailsJSON, _ := json.Marshal(req.Details)
		cType := C.CString(string(req.Type))
		cDesc := C.CString(req.Description)
		cDetails := C.CString(string(detailsJSON))
		defer C.free(unsafe.Pointer(cType))
		defer C.free(unsafe.Pointer(cDesc))
		defer C.free(unsafe.Pointer(cDetails))

		ret := C.oshin_check_perm(cType, cDesc, cDetails)
		return ret == 1
	}

	// 解析 mode
	mode := modeRaw
	action := ""
	if strings.HasPrefix(modeRaw, "route:") {
		mode = "route"
		action = strings.TrimPrefix(modeRaw, "route:")
	}

	req := plugin.PluginRequest{
		Script: script,
		Mode:   mode,
		Action: action,
		Params: params,
	}
	core := plugin.NewCoreWithConfig(config)
	core.SetCustomFunctionRegistry(globalCustomRegistry)
	resp := core.Execute(req)

	data, _ := json.Marshal(resp)
	return C.CString(string(data))
}

// 注册自定义函数。宿主程序在 Execute 前调用，注册的全局函数在 Lua 中可直接调用。
// 参数: name=函数名, arg_count=期望参数个数
//
//export OShinRegisterCustomFunction
func OShinRegisterCustomFunction(cName *C.char, cArgCount C.int) {
	name := C.GoString(cName)
	globalCustomRegistry.Register(name, int(cArgCount))
}

// 前端循环调用：阻塞等待一个待处理的自定义函数调用请求，超时返回 NULL。
// 返回 JSON: {"id":"...","name":"...","args":[...]}，需调用 OShinFreeString 释放。
//
//export OShinWaitCustomFunction
func OShinWaitCustomFunction(cTimeoutMs C.longlong) *C.char {
	timeout := time.Duration(cTimeoutMs) * time.Millisecond
	req, ok := globalCustomRegistry.WaitRequest(timeout)
	if !ok {
		return nil
	}
	data, _ := json.Marshal(req)
	return C.CString(string(data))
}

// 前端返回自定义函数调用结果，唤醒等待中的 Lua 调用。
// 参数: id=请求ID, result_json=结果JSON, error_msg=错误信息(可为NULL)
// 返回 1=已交付, 0=无效ID
//
//export OShinReturnCustomFunctionResult
func OShinReturnCustomFunctionResult(cID *C.char, cResultJSON *C.char, cError *C.char) C.int {
	id := C.GoString(cID)
	errMsg := ""
	if cError != nil {
		errMsg = C.GoString(cError)
	}

	var value interface{}
	if cResultJSON != nil {
		resultJSON := C.GoString(cResultJSON)
		if resultJSON != "" {
			_ = json.Unmarshal([]byte(resultJSON), &value)
		}
	}

	if globalCustomRegistry.ReturnResult(id, value, errMsg) {
		return 1
	}
	return 0
}

//export OShinFreeString
func OShinFreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

//export OShinVersion
func OShinVersion() *C.char {
	return C.CString(version)
}

func main() {}

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// promptModeHeaderTest 发一次对话请求，捕获出站 body。
// 返回 (含网关人格文本, 含客户端原 system 文本)——出站协议字段顺序不定，
// 用包含性断言替代位置断言。
func promptModeHeaderOutbound(t *testing.T, globalMode, globalText, header string) (bool, bool) {
	t.Helper()
	var outbound []byte
	up := &upstream.Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			outbound, _ = io.ReadAll(r.Body)
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseOK)),
			}, nil
		})},
	}
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	h := NewHandler(Config{Pool: p, Upstream: up, PromptMode: globalMode, PromptText: globalText})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"glm-5.2","messages":[{"role":"system","content":"CLIENT-SYS-MARKER"},{"role":"user","content":"hi"}]}`))
	if header != "" {
		req.Header.Set("X-Prompt-Mode", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	s := string(outbound)
	if !strings.Contains(s, `"content":"hi"`) {
		t.Fatalf("user message lost in outbound: %s", s)
	}
	return strings.Contains(s, "GATEWAY-PERSONA-MARKER"), strings.Contains(s, "CLIENT-SYS-MARKER")
}

// 全局 custom、无头：网关人格替换客户端 system（原有行为不因 fork 改变）。
func TestPromptModeHeaderAbsentKeepsGlobalCustom(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "custom", "GATEWAY-PERSONA-MARKER", "")
	if !gw || client {
		t.Fatalf("gateway=%v client=%v，want gateway 文本替换且客户端 system 被清除", gw, client)
	}
}

// 全局 custom + 头 passthrough：本请求透传客户端 system（fork 核心场景：
// 编码 agent 走全局人格伪装，ERP 等 OpenAI 直连客户端带头即透传）。
func TestPromptModeHeaderPassthroughOverridesCustom(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "custom", "GATEWAY-PERSONA-MARKER", "passthrough")
	if gw || !client {
		t.Fatalf("gateway=%v client=%v，want 客户端 system 原样透传、无网关文本", gw, client)
	}
}

// 头值大小写/首尾空白容错。
func TestPromptModeHeaderValueNormalized(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "custom", "GATEWAY-PERSONA-MARKER", "  Passthrough ")
	if gw || !client {
		t.Fatalf("gateway=%v client=%v，want passthrough 生效", gw, client)
	}
}

// 全局 passthrough + 头 custom：单请求切回人格替换（依赖 normalizePrompt
// 的 fork 变更：passthrough 全局下文本也已加载）。
func TestPromptModeHeaderCustomOverridesPassthrough(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "passthrough", "GATEWAY-PERSONA-MARKER", "custom")
	if !gw || client {
		t.Fatalf("gateway=%v client=%v，want custom 生效于单请求", gw, client)
	}
}

// 非法头值：静默回落全局，不打挂请求。
func TestPromptModeHeaderInvalidFallsBackToGlobal(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "custom", "GATEWAY-PERSONA-MARKER", "yolo")
	if !gw || client {
		t.Fatalf("gateway=%v client=%v，want 非法值回落全局 custom", gw, client)
	}
}

// 全局 passthrough、无头：透传（原有行为回归）。
func TestPromptModeGlobalPassthroughNoHeader(t *testing.T) {
	gw, client := promptModeHeaderOutbound(t, "passthrough", "GATEWAY-PERSONA-MARKER", "")
	if gw || !client {
		t.Fatalf("gateway=%v client=%v，want 全局透传", gw, client)
	}
}

package main

import (
	"strings"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/jobstore"
)

// TestDecodeStrictJSON 严格解码的边界。
func TestDecodeStrictJSON(t *testing.T) {
	type payload struct {
		Kind string `json:"kind"`
	}
	cases := []struct {
		name    string
		body    string
		wantErr bool
		want    string
	}{
		{name: "单段", body: `{"kind":"dir"}`, want: "dir"},
		{name: "未知字段", body: `{"kinds":"dir"}`, wantErr: true},
		{name: "语法错误", body: `{"kind":`, wantErr: true},
		{name: "空体", body: ``, wantErr: true},
		// 下面三条是重点：Decoder 只读第一个值，尾巴上挂什么它都不管。
		{name: "两个对象", body: `{"kind":"dir"}{"kind":"s3"}`, wantErr: true},
		{name: "尾随垃圾", body: `{"kind":"dir"}xxx`, wantErr: true},
		{name: "尾随数组", body: `{"kind":"dir"}[1,2]`, wantErr: true},
		{name: "对象后换行再一个", body: "{\"kind\":\"dir\"}\n{\"kind\":\"s3\"}", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got payload
			err := decodeStrictJSON([]byte(tc.body), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应报错，实际接受并解出 kind=%q", got.Kind)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got.Kind != tc.want {
				t.Errorf("kind = %q，期望 %q", got.Kind, tc.want)
			}
		})
	}
}

// TestDecodeStrictJSONTrailingIsNotSilentlyIgnored 钉住那个静默失败。
//
// 严格解码只做一半（拒未知字段、不拒尾随内容）时，调用方拼错请求体却拿到
// "成功"，错误要到几秒之后的任务日志里才显形，而那里根本看不出是请求体有两段。
// 这条用例就是为了让那种退化立刻失败。
func TestDecodeStrictJSONTrailingIsNotSilentlyIgnored(t *testing.T) {
	var got struct {
		Kind string `json:"kind"`
	}
	body := `{"kind":"dir"}{"kind":"stream"}`
	err := decodeStrictJSON([]byte(body), &got)
	if err == nil {
		t.Fatalf("两段 JSON 被静默接受，采用了第一段 kind=%q", got.Kind)
	}
	if !strings.Contains(err.Error(), "只有一段") {
		t.Errorf("错误信息应点明多段问题，实际: %v", err)
	}
}

// TestDecodeStrictJSONRejectsUnknown 未知字段仍要拒，别让尾随检查把严格模式顶掉。
func TestDecodeStrictJSONRejectsUnknown(t *testing.T) {
	// 目标必须是 struct。encoding/json 的 DisallowUnknownFields 对 map 目标是
	// 空操作——map 的任意 key 都算"已知"，写进 map 什么都不会报。两个真实调用
	// 点解的都是 struct（submitRequest 与 Config），所以这里也跟着用 struct。
	var got struct {
		Kind string `json:"kind"`
	}
	err := decodeStrictJSON([]byte(`{"nope":1}`), &got)
	if err == nil {
		t.Fatal("未知字段被接受")
	}
	// 错误必须来自解码本身（"unknown field"），而不是尾随检查——后者会给出
	// "只有一段"那种信息，与实际病因不符。
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("错误应来自未知字段检查，实际: %v", err)
	}
}

// TestSubmitRejectsTrailingJSON 端到端确认：请求体有两段 JSON 时必须 400。
//
// 只测 decodeStrictJSON 不够——helper 正确但端点没走它，缺陷照样存在。
// 这里发一个"看起来正常、尾巴上多挂一个对象"的请求，那种请求在真实客户端
// 里来自字符串拼接出错或中间层重试拼装。
func TestSubmitRejectsTrailingJSON(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	valid := submitBodyWith(t, "pdf", "dir")
	cases := map[string]string{
		"两个对象": valid + `{"output":{"kind":"stream"}}`,
		"尾随垃圾": valid + `}}}`,
		"尾随数组": valid + `[1]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if got.status != fasthttp.StatusBadRequest {
				t.Errorf("状态码 = %d，期望 400；尾随内容被静默接受了", got.status)
			}
		})
	}
	// 一个都不能入队——静默接受比报错更糟的地方就在这里。
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if total := depth[jobstore.StateQueued] + depth[jobstore.StateRunning]; total != 0 {
		t.Errorf("队列里有 %d 个任务，期望 0", total)
	}
}

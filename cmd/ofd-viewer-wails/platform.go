package main

import "runtime"

type platformCapabilities struct {
	Android bool `json:"android"`
	Print   bool `json:"print"`
}

// Platform 返回宿主能力，不以浏览器 User-Agent 推断原生平台。
func (d *Desktop) Platform() platformCapabilities {
	return platformCapabilities{Android: runtime.GOOS == "android", Print: runtime.GOOS != "android"}
}

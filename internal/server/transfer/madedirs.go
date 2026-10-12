package transfer

import "sync"

// madeDirs 记住"这个远端目录我们已经建过了"。
//
// 为什么需要它：逐页输出会把同一个任务的几百个文件写进同一个目录，而三个
// 协议的建目录都是每个文件跑一次。FTP 每级目录一次 MakeDir（已存在时还要
// 再列一次目录来区分 550），SFTP 与 WebDAV 每级一次 MkdirAll。4 级路径乘
// 200 个文件就是 800 次纯浪费的往返——每次都是一次网络 RTT。
//
// 粒度是"每个 Sink 实例"：`WithBaseDir` 逐字段构造新实例，所以派生实例天然
// 拿到一份空记忆，也就是每个任务从头开始。这一点是安全性的一部分——
// 记忆里若混进了别的任务建的目录，而那个目录又被第三方删了，记忆就会
// 让本次任务一直失败。限定在单个任务内，最坏情况是这一个任务失败一次
// （错误是明确的"目录不存在"），重试即恢复。
//
// 记忆的内容是"服务端存在这个目录"这一事实，与用哪条连接无关，所以连接池
// 复用不会让它失效。
type madeDirs struct {
	mu   sync.Mutex
	seen map[string]bool
}

// has 判断目录是否已建过。
func (m *madeDirs) has(dir string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen[dir]
}

// add 记住目录已建。
func (m *madeDirs) add(dir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil {
		m.seen = make(map[string]bool, 8)
	}
	m.seen[dir] = true
}

// invalidate 清掉记忆。用于建目录失败时：那次调用没有真的把目录建出来，
// 记忆里若已有它，下次不该被跳过。
func (m *madeDirs) invalidate(dir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.seen, dir)
}

// forgetAll 清空记忆。
//
// 唯一的用处是"上传失败了，重来一次"：失败原因可能是远端目录在我们建完之后
// 又被删了，这时记忆已经不可信，应该重新走一遍建目录的流程。
func (m *madeDirs) forgetAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = nil
}

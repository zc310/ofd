package container

import "io"

// maxReadInitialCapacity 只限制声明大小触发的预分配，不改变实际读取及限流语义。
// ZIP 元数据不可信，不能按任意声明值一次性分配整块内存。
const maxReadInitialCapacity = 1 << 20

// readAllHint 用大小提示减少正常 ZIP 条目的扩容复制，仍必须读到真正的 EOF。
// 额外留一字节供读取器触发 CRC、数据描述符及声明大小校验，不能读满提示后就返回。
func readAllHint(reader io.Reader, hint uint64) ([]byte, error) {
	if hint == 0 {
		return io.ReadAll(reader)
	}
	data := make([]byte, 0, int(min(hint, maxReadInitialCapacity))+1)
	for {
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
		n, err := reader.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
	}
}

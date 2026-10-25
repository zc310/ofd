package fontrepair

import "fmt"

// ExtractCollectionFace 根据 OpenType 1.9.1 Font Collections / TTC Header 与
// Table Directory 规则提取指定字体面。目录和共享表偏移相对于整个集合文件；
// 输出重新计算表偏移与校验和，不修改字体表内容、字形编号或 CFF 子程序。
func ExtractCollectionFace(data []byte, index int) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "ttcf" {
		return nil, fmt.Errorf("font collection: truncated or invalid header")
	}
	version := be.Uint32(data[4:])
	if version != 0x00010000 && version != 0x00020000 {
		return nil, fmt.Errorf("font collection: unsupported version")
	}
	count := be.Uint32(data[8:])
	if count == 0 || uint64(count)*4 > uint64(len(data)-12) {
		return nil, fmt.Errorf("font collection: truncated offset array")
	}
	if index < 0 || uint64(index) >= uint64(count) {
		return nil, fmt.Errorf("font collection: face index %d out of range", index)
	}
	offset := be.Uint32(data[12+index*4:])
	header, ok := sliceAt(data, offset, 12)
	if !ok || !sfntSignature(header) {
		return nil, fmt.Errorf("font collection: invalid face directory")
	}
	d, err := readDirectoryAt(data, offset)
	if err != nil {
		return nil, err
	}
	if len(d.tables) == 0 {
		return nil, fmt.Errorf("font collection: empty face directory")
	}
	return d.encode(), nil
}

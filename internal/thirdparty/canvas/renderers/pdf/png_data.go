package pdf

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
)

type pngRasterInfo struct {
	bits         int
	compressed   []byte
	palette      []byte
	transparency []byte
}

// pngRasterData 为 RGBA/Indexed 快路径校验 PNG 块结构；仅接受非交错图片。
// RGBA 只接收 8 位；调色板接收 1/2/4/8 位索引，PLTE/tRNS 必须位于 IDAT 之前。
func pngRasterData(data []byte, size image.Point, colorType byte) (pngRasterInfo, bool) {
	var info pngRasterInfo
	if size.X <= 0 || size.Y <= 0 || len(data) < 33 || string(data[:8]) != "\x89PNG\r\n\x1a\n" ||
		binary.BigEndian.Uint32(data[8:12]) != 13 || string(data[12:16]) != "IHDR" ||
		uint64(binary.BigEndian.Uint32(data[16:20])) != uint64(size.X) ||
		uint64(binary.BigEndian.Uint32(data[20:24])) != uint64(size.Y) ||
		data[25] != colorType || !bytes.Equal(data[26:29], []byte{0, 0, 0}) {
		return info, false
	}
	info.bits = int(data[24])
	if colorType == 6 {
		if info.bits != 8 {
			return info, false
		}
	} else if colorType != 3 || (info.bits != 1 && info.bits != 2 && info.bits != 4 && info.bits != 8) {
		return info, false
	}
	var compressed bytes.Buffer
	seenData, endedData := false, false
	for pos := 8; len(data)-pos >= 12; {
		length := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if length > uint64(len(data)-pos-12) {
			return info, false
		}
		end := pos + 8 + int(length)
		if crc32.ChecksumIEEE(data[pos+4:end]) != binary.BigEndian.Uint32(data[end:end+4]) {
			return info, false
		}
		kind := string(data[pos+4 : pos+8])
		payload := data[pos+8 : end]
		if seenData && kind != "IDAT" {
			endedData = true
		}
		switch kind {
		case "IHDR":
			if pos != 8 {
				return info, false
			}
		case "PLTE":
			if colorType != 3 || info.palette != nil || seenData ||
				length == 0 || length%3 != 0 || length/3 > uint64(1<<info.bits) {
				return info, false
			}
			info.palette = payload
		case "tRNS":
			if colorType != 3 || info.transparency != nil || seenData ||
				length == 0 || length > uint64(len(info.palette)/3) {
				return info, false
			}
			info.transparency = payload
		case "IDAT":
			if endedData || (colorType == 3 && info.palette == nil) {
				return info, false
			}
			seenData = true
			compressed.Write(payload)
		case "IEND":
			if length != 0 || end+4 != len(data) || compressed.Len() == 0 {
				return info, false
			}
			info.compressed = compressed.Bytes()
			return info, true
		default:
			if data[pos+4]&0x20 == 0 {
				return info, false
			}
		}
		pos = end + 4
	}
	return info, false
}

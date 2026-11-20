package canvasfont

import (
	"errors"
	"os"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

var errExternalFontUnusable = errors.New("外部字体结构不可用")

// loadExternalFont 统一磁盘与内存注册的外部字体入口，在发布前选择 Unicode cmap。
// 内嵌字体使用独立的修复链，不经过这里。依据 OpenType 1.9.1 cmap 的
// Encoding records and encodings，基础子表互斥，不能混用旧 Macintosh 字符码。
func loadExternalFont(family *canvas.FontFamily, data []byte, index int, style drawing.FontStyle) error {
	if err := family.LoadFont(data, index, canvasStyle(style)); err != nil {
		return err
	}
	selectUnicodeCmap(family.Face(1, canvasStyle(style)).Font)
	if !fontFamilyUsable(family) {
		return errExternalFontUnusable
	}
	return nil
}

func loadExternalFontFile(family *canvas.FontFamily, path string, style drawing.FontStyle) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return loadExternalFont(family, data, 0, style)
}

// selectUnicodeCmap 对新加载的外部字体选择单个 Unicode 字符映射。
// OpenType 1.9.1 cmap 的 Encoding records and encodings 规定各基础子表互斥，
// 全字符集 Unicode 映射优先于 BMP，不能按记录顺序混用 Macintosh 字符码。
// 当前字体库会在旧 format 0 返回 GID 0 后停止，导致 KaiTi_GB2312 的拉丁字母
// 被误判为缺字。这里只替换尚未发布的字体的 cmap 查询视图，保留原表和字形数据。
func selectUnicodeCmap(f *canvas.Font) {
	if f == nil || f.SFNT == nil || f.Cmap == nil {
		return
	}
	selected, priority := -1, 0
	for i, record := range f.Cmap.EncodingRecords {
		score := 0
		switch {
		case record.PlatformID == 3 && record.EncodingID == 10, record.PlatformID == 0 && record.EncodingID == 4:
			score = 3
		case record.PlatformID == 3 && record.EncodingID == 1,
			record.PlatformID == 0 && record.EncodingID <= 3:
			score = 2
		case record.PlatformID == 0 && record.EncodingID == 6:
			score = 1
		}
		if score > priority && int(record.Subtable) < len(f.Cmap.Subtables) {
			selected, priority = i, score
		}
	}
	if selected < 0 {
		return
	}
	cmap := *f.Cmap
	record := cmap.EncodingRecords[selected]
	cmap.Subtables = cmap.Subtables[record.Subtable : record.Subtable+1 : record.Subtable+1]
	record.Subtable = 0
	cmap.EncodingRecords = append(cmap.EncodingRecords[:0:0], record)
	f.Cmap = &cmap
}

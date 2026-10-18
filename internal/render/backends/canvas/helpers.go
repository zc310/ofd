package canvas

// containsPrivateGlyphRune 判断文本是否包含私有区字形引用。
func containsPrivateGlyphRune(value string) bool {
	for _, r := range value {
		if r >= 0xF0000 && r <= 0xFFFFD {
			return true
		}
	}
	return false
}

package compare

import (
	"regexp"
	"strings"
	"testing"
)

func TestSnapshotDoesNotSilentlyDropUnreadableTemplateText(t *testing.T) {
	valid := testOFD(t, "text")
	common := regexp.MustCompile(`(<(?:[A-Za-z_][\w]*:)?CommonData\b[^>]*>)`)
	page := regexp.MustCompile(`(<(?:[A-Za-z_][\w]*:)?Page\b[^>]*>)`)
	area := regexp.MustCompile(`<(?:[A-Za-z_][\w]*:)?Area\b`)
	modified := 0
	broken := rewrite(t, valid, func(name string, data []byte) (string, []byte) {
		text := string(data)
		if strings.HasSuffix(name, "Document.xml") {
			text = common.ReplaceAllString(text, `${1}<TemplatePage ID="900" BaseLoc="missing.xml"/>`)
		} else if page.MatchString(text) {
			injection := `${1}<Template TemplateID="900"/>`
			if !area.MatchString(text) {
				injection += `<Area><PhysicalBox>0 0 30 20</PhysicalBox></Area>`
			}
			text = page.ReplaceAllString(text, injection)
		}
		if text != string(data) {
			modified++
		}
		return name, []byte(text)
	})
	if modified != 2 {
		t.Fatalf("回归样本未成功注入缺失模板：%d", modified)
	}
	if _, err := CompareFiles(t.Context(), []string{valid, broken}, Options{}); err == nil || !strings.Contains(err.Error(), "文字") {
		t.Fatalf("比较快照吞掉了模板文字加载错误：%v", err)
	}
}

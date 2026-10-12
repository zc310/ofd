package xps

import (
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

func TestXPSImporterRegistered(t *testing.T) {
	imp, ok := converter.ImporterByName("xps")
	if !ok || imp.Name() != "xps" {
		t.Fatalf("XPS 导入器未注册: %v", imp)
	}
	if byExt, ok := converter.ImporterByExtension(".XPS"); !ok || byExt.Name() != "xps" {
		t.Fatalf(".xps 导入器未注册: %v", byExt)
	}
}

func TestXPSTransformerRegistered(t *testing.T) {
	transformer, ok := converter.TransformerFor("xps", "pdf")
	if !ok || transformer.To() != "pdf" {
		t.Fatal("XPS→PDF 转换器未注册")
	}
}

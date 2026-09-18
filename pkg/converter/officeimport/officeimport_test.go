package officeimport_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/office"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/officeimport"
	"github.com/zc310/ofd/pkg/validator"
)

func TestOfficeImporterRegistered(t *testing.T) {
	for _, name := range []string{"docx", "doc", "odt", "rtf", "wps", "pptx", "ppt", "odp", "xlsx", "xls", "ods"} {
		if _, ok := converter.ImporterByName(name); !ok {
			t.Fatalf("未注册 Office 导入器: %s", name)
		}
	}
	if imp, ok := converter.ImporterByExtension(".docx"); !ok || imp.Name() != "docx" {
		t.Fatalf("按扩展名查找 docx 导入器失败: %v %v", imp, ok)
	}
}

func TestOfficeTransformerRegistered(t *testing.T) {
	for _, name := range []string{"docx", "doc", "pptx", "xlsx"} {
		if _, ok := converter.TransformerFor(name, "pdf"); !ok {
			t.Fatalf("未注册 %s→pdf 直接转换器", name)
		}
	}
	if _, ok := converter.TransformerFor("docx", "ofd"); ok {
		t.Fatal("docx→ofd 不应注册直接转换器")
	}
}

func TestFindSofficeReportsMissingPath(t *testing.T) {
	if _, err := office.FindSoffice(filepath.Join(t.TempDir(), "no-such-soffice")); err == nil {
		t.Fatal("指定的不存在的 soffice 路径应报错")
	}
}

func TestConvertDocxToPDFAndOFD(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("未安装 LibreOffice，跳过")
	}
	docx := writeTestDocx(t)
	var pdf bytes.Buffer
	if err := converter.Convert("docx", "pdf", docx, &pdf); err != nil {
		t.Fatalf("docx→pdf 失败: %v", err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("docx→pdf 输出不是 PDF")
	}
	var ofd bytes.Buffer
	if err := converter.Convert("docx", "ofd", docx, &ofd); err != nil {
		t.Fatalf("docx→ofd 失败: %v", err)
	}
	if !bytes.HasPrefix(ofd.Bytes(), []byte("PK")) {
		t.Fatalf("docx→ofd 输出不是 OFD")
	}
	validatorInstance, err := validator.New()
	if err != nil {
		t.Fatalf("创建校验器失败: %v", err)
	}
	report := validatorInstance.ValidateReader(context.Background(), bytes.NewReader(ofd.Bytes()), "converted.ofd")
	if report.Summary.Errors != 0 {
		t.Fatalf("OFD 校验存在错误: %d", report.Summary.Errors)
	}
}

// writeTestDocx 在临时目录生成一个最小 docx，包含一段中文与英文文本。
// 先生成扁平 ODT（.fodt，单个 XML 文件），再由 LibreOffice 转为 docx。
func writeTestDocx(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.fodt")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" office:version="1.2">
<office:body><office:text><text:p>Office 转换测试 Hello</text:p></office:text></office:body>
</office:document>`
	if err := os.WriteFile(source, []byte(content), 0600); err != nil {
		t.Fatalf("写入 fodt 失败: %v", err)
	}
	// 用 LibreOffice 把 fodt 转成 docx，作为稳定的测试输入。
	outDir := t.TempDir()
	cmd := exec.Command("soffice", "--headless", "--norestore", "--nolockcheck", "--nodefault", "--nologo",
		"-env:UserInstallation=file://"+filepath.ToSlash(filepath.Join(t.TempDir(), "profile")),
		"--convert-to", "docx", "--outdir", outDir, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("生成 docx 测试输入失败: %v: %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "source.docx"))
	if err != nil {
		t.Skipf("读取生成的 docx 失败: %v", err)
	}
	return data
}

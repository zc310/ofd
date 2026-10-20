package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/version"
)

const invoiceAttachment = `<?xml version="1.0" encoding="UTF-8"?>
<Invoice>
  <MachineNo>444000224046</MachineNo>
  <InvoiceCode>033001900111</InvoiceCode>
  <InvoiceNo>12345678</InvoiceNo>
  <IssueDate>2026-09-18</IssueDate>
  <InvoiceCheckCode>6A5B2C3D1E4F</InvoiceCheckCode>
  <TaxExclusiveTotalAmount>100.00</TaxExclusiveTotalAmount>
  <TaxTotalAmount>13.00</TaxTotalAmount>
  <TaxInclusiveTotalAmount>113.00</TaxInclusiveTotalAmount>
  <Payee>王五</Payee>
  <Checker>赵六</Checker>
  <InvoiceClerk>张三</InvoiceClerk>
  <Buyer><BuyerName>买方公司</BuyerName><BuyerTaxID>911100000000000000</BuyerTaxID></Buyer>
  <Seller><SellerName>卖方公司</SellerName><SellerTaxID>911100001111000000</SellerTaxID></Seller>
  <GoodsInfos>
    <G1><Item>办公用品*中性笔</Item><Quantity>10.00</Quantity><Price>2.00</Price>
      <Amount>20.00</Amount><TaxScheme>13%</TaxScheme><TaxAmount>2.60</TaxAmount></G1>
  </GoodsInfos>
</Invoice>`

// buildInvoiceOFD 生成带发票附件的最小 OFD 包。
func buildInvoiceOFD(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml": `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
  <DocBody><DocInfo><DocID>invoice</DocID><Title>发票</Title></DocInfo>
  <DocRoot>Doc_0/Document.xml</DocRoot></DocBody>
</OFD>`,
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="http://www.ofdspec.org/2016">
  <CommonData><MaxUnitID>10</MaxUnitID>
    <PageArea><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></PageArea>
  </CommonData>
  <Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>
</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Page xmlns="http://www.ofdspec.org/2016">
  <Area><PhysicalBox>0 0 210 297</PhysicalBox><ApplicationBox>0 0 210 297</ApplicationBox></Area>
  <Content>
    <Layer ID="1" Type="Body">
      <TextObject ID="1" Boundary="70 10 70 8" Font="1" Size="4"><TextCode X="30" Y="20">增值税电子普通发票</TextCode></TextObject>
    </Layer>
  </Content>
</Page>`,
		"Doc_0/Attachs/original_invoice.xml": invoiceAttachment,
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, value := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func writeTempOFD(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invoice.ofd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunExtractsInvoiceToStdout(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{`"number":"12345678"`, `"code":"033001900111"`, `"type":"普通发票"`, `"title":"增值税电子普通发票"`} {
		if !strings.Contains(text, want) {
			t.Errorf("stdout 缺少 %s\n%s", want, text)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want 空", stderr.String())
	}
}

func TestRunExtractsCustomTagInvoice(t *testing.T) {
	const input = "../../testdata/ofdrw/20240531141733.ofd"
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 OFD 测试文件：20240531141733.ofd")
	} else if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{input}, &stdout, &stderr)
	if code != exitOK || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{
		`"number":"24112000000048542163"`, `"type":"专用发票"`,
		`"total_amount":"4449"`, `"attachment":"Doc_0/Tags/CustomTag.xml"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("stdout 缺少 %s\n%s", want, text)
		}
	}
	if strings.Contains(text, `"items"`) || strings.Contains(text, `"summary"`) {
		t.Errorf("单张输入应输出 Invoice：%s", text)
	}
}

func TestRunPrettyOutputIndents(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"--pretty", path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\n  \"") {
		t.Errorf("--pretty 应输出缩进 JSON")
	}
}

func TestRunWritesOutputFile(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	output := filepath.Join(t.TempDir(), "invoice.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", output, path}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want 空", stdout.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"number":"12345678"`) {
		t.Errorf("输出文件缺少发票号码字段")
	}
}

func TestRunMissingInputIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "缺少输入") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TestRunMultipleInputsAreAccepted 记录位置参数不再限一个：批量汇总与合并
// 都要靠多个输入路径，参数个数本身不再是错误。
func TestRunMultipleInputsAreAccepted(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.ofd")
	second := filepath.Join(dir, "b.ofd")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, buildInvoiceOFD(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{first, second}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{`"items"`, `"summary"`, `"total_amount":"226"`} {
		if !strings.Contains(text, want) {
			t.Errorf("批量 JSON 缺少 %s\n%s", want, text)
		}
	}
}

// TestRunSingleInputKeepsInvoiceShape 记录单张输入的输出仍是单个 Invoice 对象，
// 已有脚本按此结构取值。
func TestRunSingleInputKeepsInvoiceShape(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	if strings.Contains(text, `"summary"`) || strings.Contains(text, `"items"`) {
		t.Errorf("单张输入应输出单个 Invoice，不含批量字段：%s", text)
	}
	if !strings.Contains(text, `"total_amount":"113"`) {
		t.Errorf("单张输出缺少金额字段：%s", text)
	}
}

// TestRunSkipBrokenContinues 确认 --skip-broken 让坏票不再中断整批。
func TestRunSkipBrokenContinues(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.ofd")
	if err := os.WriteFile(good, buildInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.ofd")
	if err := os.WriteFile(broken, buildNotInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--skip-broken", good, broken}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "broken.ofd") {
		t.Errorf("失败票应出现在结果里：%s", stdout.String())
	}
}

// TestRunBatchIsStrictByDefault 确认默认严格：坏票让整批失败。
func TestRunBatchIsStrictByDefault(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.ofd")
	if err := os.WriteFile(good, buildInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.ofd")
	if err := os.WriteFile(broken, buildNotInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{good, broken}, &stdout, &stderr); code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want 空", stdout.String())
	}
}

// TestRunWritesCSVReport 确认 --format csv 输出报表，格式也可由扩展名推断。
func TestRunWritesCSVReport(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	dir := t.TempDir()
	output := filepath.Join(dir, "report.csv")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-o", output, path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"发票代码", "12345678", "合计 1 张", "113.00"} {
		if !strings.Contains(text, want) {
			t.Errorf("CSV 缺少 %s\n%s", want, text)
		}
	}
}

// TestRunWritesXLSXReport 确认 --format xlsx 输出工作簿（ZIP 头即合法 XLSX）。
func TestRunWritesXLSXReport(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	dir := t.TempDir()
	output := filepath.Join(dir, "report.xlsx")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-o", output, path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 || string(data[:2]) != "PK" {
		t.Errorf("XLSX 应是 ZIP 包，实际前两字节 %q", data[:min(len(data), 2)])
	}
}

// TestRunRejectsUnknownFormat 确认未知格式是参数错误而不是回退成 JSON。
func TestRunRejectsUnknownFormat(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format", "xhtml", path}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

// TestRunRejectsPrettyForCSV 确认 --pretty 与报表格式互斥。
func TestRunRejectsPrettyForCSV(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"--pretty", "--format", "csv", path}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

// TestRunMergesPDF 确认 --merge-pdf 合并输出，且页数等于发票张数。
func TestRunMergesPDF(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.ofd")
	second := filepath.Join(dir, "b.ofd")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, buildInvoiceOFD(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "merged.pdf")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--merge-pdf", output, first, second}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		t.Fatalf("输出不是 PDF：%q", data[:min(len(data), 5)])
	}
	if got := countPages(data); got != 2 {
		t.Errorf("合并后应有 2 页，实际 %d", got)
	}
	// 合并 PDF 时 JSON 结果仍是批量形态，含汇总金额。
	if !strings.Contains(stdout.String(), `"total_amount":"226"`) {
		t.Errorf("合并时的汇总金额缺失：%s", stdout.String())
	}
}

// TestRunCoverRequiresMergePDF 确认 --cover 单独使用是参数错误。
func TestRunCoverRequiresMergePDF(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--cover", path}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

// TestRunRejectsUnknownSort 确认未知排序方式是参数错误。
func TestRunRejectsUnknownSort(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--sort", "size", path}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

// TestRunInputDirCollectsInvoices 确认 --input-dir 按名称收集目录内的发票。
func TestRunInputDirCollectsInvoices(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.ofd", "a.ofd", "note.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), buildInvoiceOFD(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--input-dir", dir, "--sort", "name"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, `"total_amount":"226"`) {
		t.Errorf("应汇总目录内两张发票：%s", text)
	}
	if !strings.Contains(text, "a.ofd") || !strings.Contains(text, "b.ofd") {
		t.Errorf("应包含目录内全部发票：%s", text)
	}
	if strings.Contains(text, "note.txt") {
		t.Errorf("非发票文件不应被收集：%s", text)
	}
}

// TestRunOutputCannotOverwriteMergePDF 确认输出与合并 PDF 不能是同一个文件。
func TestRunOutputCannotOverwriteMergePDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ofd")
	if err := os.WriteFile(path, buildInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	merged := filepath.Join(dir, "merged.pdf")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", merged, "--merge-pdf", merged, path}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

// TestRunDedupSkipsRepeat 确认 --dedup 让重复票不计入合计。
func TestRunDedupSkipsRepeat(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.ofd")
	again := filepath.Join(dir, "a-copy.ofd")
	for _, path := range []string{first, again} {
		if err := os.WriteFile(path, buildInvoiceOFD(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--dedup", first, again}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, `"total_amount":"113"`) {
		t.Errorf("去重后合计应为 113：%s", text)
	}
	if !strings.Contains(text, "skipped") {
		t.Errorf("去重项应记入 skipped：%s", text)
	}
}

// buildNotInvoiceOFD 生成不含发票附件的 OFD 包。
func buildNotInvoiceOFD(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	writer, err := archive.Create("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("<OFD/>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// pagePattern 匹配 PDF 页对象，Pages 是页树根节点，不算页。
var pagePattern = regexp.MustCompile(`/Type\s*/Page[^s]`)

// countPages 统计 PDF 页数。
func countPages(data []byte) int {
	return len(pagePattern.FindAll(data, -1))
}

func TestRunMissingInputFileIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "missing.ofd")}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "输入文件") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunNonInvoiceIsExitError(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	writer, err := archive.Create("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("<OFD/>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := writeTempOFD(t, data.Bytes())
	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "附件") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want 空", stdout.String())
	}
}

func TestRunOutputOverwritingInputIsUsageError(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", path, path}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunHelpExitsOK(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "用法") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

// TestRunVersionPrintsBuildVersion 记录 --version 输出发行版本且不进入抽取流程：
// 它必须能独立于输入文件与参数校验工作。
func TestRunVersionPrintsBuildVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--version", "--format", "csv"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("run(%v) = %d，stderr = %q", args, code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != version.Version {
			t.Fatalf("run(%v) 输出 %q，期望 %q", args, stdout.String(), version.Version)
		}
	}
}

// TestRunMergePDFNupPacksPages 确认 --nup 把多张发票合成一页。
func TestRunMergePDFNupPacksPages(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.ofd")
	second := filepath.Join(dir, "b.ofd")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, buildInvoiceOFD(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "merged.pdf")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--merge-pdf", output, "--nup", "2", first, second},
		&stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPages(data); got != 1 {
		t.Errorf("两联拼版后应为 1 页，实际 %d", got)
	}
	// 两格默认纵向 A4 单列拼接：票源多为宽而矮的版式，纵向能保持原始尺寸。
	if !strings.Contains(string(data), "/MediaBox[0 0 595.27559 841.88976]") {
		t.Error("两格拼版应是纵向 A4")
	}
}

// TestRunMergePDFLayoutFlagsNeedMergePDF 确认版面类选项必须配合 --merge-pdf。
func TestRunMergePDFLayoutFlagsNeedMergePDF(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	for _, args := range [][]string{
		{"--pages", "first", path},
		{"--paper", "A4", path},
		{"--orientation", "landscape", path},
		{"--fit", "contain", path},
		{"--nup", "2", path},
		{"--nup-gap", "5", path},
		{"--cell-border", path},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("run(%v) = %d，期望 %d", args, code, exitUsage)
		}
	}
}

// TestRunMergePDFRejectsBadLayout 确认非法版面参数是抽取失败（退出码 1）而不是崩溃。
func TestRunMergePDFRejectsBadLayout(t *testing.T) {
	path := writeTempOFD(t, buildInvoiceOFD(t))
	output := filepath.Join(t.TempDir(), "merged.pdf")
	for _, args := range [][]string{
		{"--merge-pdf", output, "--nup", "5", path},
		{"--merge-pdf", output, "--paper", "A0", path},
		{"--merge-pdf", output, "--fit", "cover", path},
		{"--merge-pdf", output, "--pages", "9", path},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitError {
			t.Errorf("run(%v) = %d，期望 %d", args, code, exitError)
		}
	}
}

// TestRunBooleanFlagWithSeparateValueExplainsIt 确认误写成 "--cover true" 时
// 错误信息指向真实原因。
//
// pflag 不会把布尔选项后面那个词吃成选项值，于是 true 变成位置参数，报出来是
// "找不到 true 这个文件"——与真实原因毫无关联，用户很容易去建一个叫 true 的文件。
func TestRunBooleanFlagWithSeparateValueExplainsIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ofd"), buildInvoiceOFD(t), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--input-dir", dir, "--cover", "true"},
		{"--input-dir", dir, "--skip-broken", "false"},
		{"--input-dir", dir, "true"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		if code != exitUsage {
			t.Errorf("run(%v) = %d，期望 %d", args, code, exitUsage)
		}
		if !strings.Contains(stderr.String(), "布尔选项") {
			t.Errorf("run(%v) 的错误信息应说明布尔选项的写法，实际：%q", args, stderr.String())
		}
	}
}

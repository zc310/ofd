// Package csv 注册 CSV 和 TSV 表格导入器，以 Go 完成断行、分页和 OFD 写出。
// 首行默认作为续页重复的表头；需要时通过 converter.WithCSVImportOptions 配置。
//
//	import _ "github.com/zc310/ofd/pkg/converter/import/csv"
//
// converter.Convert(ctx, "csv", "ofd", input, output) 接受路径、[]byte 或 io.Reader。
// TSV 使用相同导入包，格式名为 "tsv"。所有字段按文字保留，不计算公式或推断数值。
package csv

import (
	"bytes"
	"context"
	stdcsv "encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	maxInputBytes = 32 << 20
	maxFontBytes  = 64 << 20
	maxRecords    = 100_000
	maxColumns    = 256
	maxCells      = 1_000_000
)

type importer struct{ tsv bool }

func (i importer) Name() string {
	if i.tsv {
		return "tsv"
	}
	return "csv"
}

func (i importer) Extensions() []string { return []string{"." + i.Name()} }
func (i importer) MIME() string {
	if i.tsv {
		return "text/tab-separated-values"
	}
	return "text/csv"
}

func (i importer) Import(input any, output io.Writer, conv *converter.Converter) (err error) {
	ctx := conv.Context()
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	opts, err := conv.CSVImportOptions().Normalize()
	if err != nil {
		return err
	}
	data, title, err := readInput(ctx, input)
	if err != nil {
		return err
	}
	if opts.Title != "" {
		title = opts.Title
	} else if title == "" {
		title = strings.ToUpper(i.Name())
	}
	data, err = decodeText(data, opts.Encoding)
	if err != nil {
		return err
	}
	comma := ','
	if i.tsv {
		comma = '\t'
	}
	records, err := readRecords(ctx, data, comma)
	if err != nil {
		return err
	}
	settings := flow.DefaultOptions()
	settings.PageSize.Width, settings.PageSize.Height = conv.Paper().EffectiveDimensions()
	settings.PageNumbers = true
	if err := configureFont(ctx, &settings, opts, records, title); err != nil {
		return err
	}
	doc := flow.New(settings)
	doc.Metadata.Title = title
	doc.Metadata.Creator, doc.Metadata.CreatorVersion = "zc310/ofd", version.Version
	doc.Metadata.CustomDatas = []creator.CustomData{{Name: "SourceFormat", Value: strings.ToUpper(i.Name())}}
	// 表题比一级标题紧凑，跨页只重复表头，保留更多空间给数据行。
	doc.AddHeading(2, title)
	table := flow.Table{Rows: records, Columns: make([]flow.TableColumn, len(records[0])), SplitRows: true}
	if !opts.NoHeader {
		table.Header, table.Rows = records[0], records[1:]
	}
	doc.AddTable(table)
	return doc.Write(ctx, output, creator.CreateOptions{Deterministic: true})
}

func readInput(ctx context.Context, input any) ([]byte, string, error) {
	var reader io.Reader
	var title string
	switch value := input.(type) {
	case string:
		file, err := os.Open(value)
		if err != nil {
			return nil, "", err
		}
		defer file.Close()
		reader = file
		title = strings.TrimSuffix(filepath.Base(value), filepath.Ext(value))
	case []byte:
		if len(value) > maxInputBytes {
			return nil, "", fmt.Errorf("CSV/TSV 输入超过 32 MiB")
		}
		return value, "", nil
	case io.Reader:
		reader = value
	default:
		return nil, "", fmt.Errorf("不支持的 CSV/TSV 输入类型 %T", input)
	}
	data, err := readLimited(ctx, reader, maxInputBytes)
	return data, title, err
}

func readLimited(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(contextio.Reader(ctx, reader), limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("输入超过大小上限 %d 字节", limit)
	}
	return data, nil
}

func decodeText(data []byte, encoding string) ([]byte, error) {
	var err error
	switch encoding {
	case "gb18030":
		data, err = simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	case "gbk":
		data, err = simplifiedchinese.GBK.NewDecoder().Bytes(data)
	}
	if err != nil {
		return nil, fmt.Errorf("CSV/TSV 解码失败: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("CSV/TSV 不是有效的 UTF-8，请显式设置输入编码")
	}
	return data, nil
}

func readRecords(ctx context.Context, data []byte, comma rune) ([][]string, error) {
	reader := stdcsv.NewReader(contextio.Reader(ctx, bytes.NewReader(data)))
	reader.Comma = comma
	var records [][]string
	cells := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV/TSV 解析失败: %w", err)
		}
		cells += len(record)
		if len(records) >= maxRecords || len(record) > maxColumns || cells > maxCells {
			return nil, fmt.Errorf("CSV/TSV 超过表格上限（100000 行、256 列或 1000000 个单元格）")
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("CSV/TSV 没有可导入的记录")
	}
	return records, nil
}

// configureFont 使用与排版度量相同的实际字体数据，并由 creator 子集化嵌入。
// 默认 Go 字体覆盖不了输入时，再查找系统中文字体；缺字由 flow 明确报告。
func configureFont(ctx context.Context, settings *flow.Options, opts converter.CSVImportOptions, records [][]string, title string) error {
	data := opts.FontData
	path := opts.FontFile
	if path == "" && len(data) == 0 {
		face, err := sfnt.Parse(goregular.TTF)
		if err != nil {
			return err
		}
		var buffer sfnt.Buffer
		covered := true
		seen := make(map[rune]bool)
		check := func(text string) {
			for _, r := range text {
				if seen[r] || r == '\n' || r == '\r' || r == '\t' {
					continue
				}
				seen[r] = true
				glyph, err := face.GlyphIndex(&buffer, r)
				covered = covered && err == nil && glyph != 0
			}
		}
		check(title)
		for _, row := range records {
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, value := range row {
				check(value)
			}
		}
		if covered {
			return nil
		}
		for _, name := range []string{"Noto Sans CJK SC", "Noto Sans SC", "Source Han Sans SC", "Microsoft YaHei", "微软雅黑", "SimSun", "宋体", "WenQuanYi Micro Hei"} {
			if found, ok := canvas.FindSystemFont(name, canvas.FontRegular); ok {
				path = found
				break
			}
		}
		if path == "" {
			return fmt.Errorf("CSV/TSV 含内置字体未覆盖的文字，请通过 csv-font / FontFile / FontData 指定字体")
		}
	}
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("读取 CSV/TSV 字体: %w", err)
		}
		defer file.Close()
		data, err = readLimited(ctx, file, maxFontBytes)
		if err != nil {
			return err
		}
	}
	if len(data) > maxFontBytes {
		return fmt.Errorf("CSV/TSV 字体超过 64 MiB")
	}
	// 使用同一实际字体面；没有提供同族粗体时不借用拉丁粗体替代中文表头。
	settings.Fonts = append(settings.Fonts, creator.Font{Name: "CSV-Regular", Data: data})
	settings.Theme.Body.Font, settings.Theme.Table.Font = "CSV-Regular", "CSV-Regular"
	settings.Theme.Caption.Font = "CSV-Regular"
	for index := range settings.Theme.Headings {
		settings.Theme.Headings[index].Font = "CSV-Regular"
	}
	settings.FallbackFonts = []string{"Flow-Regular"}
	settings.BoldFont, settings.ItalicFont, settings.BoldItalicFont = "", "", ""
	return nil
}

func init() {
	converter.RegisterImporter(importer{})
	converter.RegisterImporter(importer{tsv: true})
}

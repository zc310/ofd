package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/pkg/converter"
)

const (
	defaultDPI     = 150
	defaultBgColor = "white"
)

var (
	ErrInvalidFormat = errors.New("不支持的输出格式")
	ErrNoInput       = errors.New("未指定输入文件")
)

type options struct {
	input  string
	output string
	format string
	dpi    int
	page   int
	bg     string
	dir    bool
	help   bool
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.help {
		return
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "转换失败:", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (*options, error) {
	opts := &options{dpi: defaultDPI, bg: defaultBgColor}
	fs := flag.NewFlagSet("ofd-converter", flag.ContinueOnError)
	var output, format string
	fs.StringVar(&output, "o", "", "输出文件路径或目录，多页图片时可为 .zip 文件或目录")
	fs.StringVar(&output, "output", "", "输出文件路径或目录，多页图片时可为 .zip 文件或目录")
	fs.StringVar(&format, "format", "", "输出格式: pdf, txt, png, jpg, svg, eps, tex")
	fs.IntVar(&opts.dpi, "dpi", defaultDPI, "输出分辨率 (1-1200)")
	fs.IntVar(&opts.page, "page", 0, "指定转换的页码 (从 1 开始)，0 表示全部页面")
	fs.StringVar(&opts.bg, "bg", defaultBgColor, "背景颜色: transparent, white, black")
	fs.BoolVar(&opts.dir, "dir", false, "不压缩，将多页图片直接保存到输出目录下的多个文件")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "ofd-converter - OFD 文档转换命令行工具\n\n用法: ofd-converter [选项] <输入文件> [输出文件或目录]\n\n选项:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return &options{help: true}, nil
		}
		return nil, err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return nil, ErrNoInput
	}
	opts.input = rest[0]
	if output != "" {
		opts.output = output
	} else if len(rest) >= 2 {
		opts.output = rest[1]
	}
	opts.format = format
	return opts, nil
}

func run(opts *options) error {
	if _, err := os.Stat(opts.input); err != nil {
		return fmt.Errorf("输入文件: %w", err)
	}
	format := strings.ToLower(strings.TrimSpace(opts.format))
	switch format {
	case "":
		if strings.EqualFold(filepath.Ext(opts.output), ".zip") {
			return errors.New("输出为 .zip 时需要通过 -format 指定图片格式")
		}
		format = formatFromExtension(opts.output)
	case "jpeg":
		format = "jpg"
	}
	switch format {
	case "pdf", "txt", "png", "jpg", "svg", "eps", "tex":
	default:
		return fmt.Errorf("%w: %s", ErrInvalidFormat, format)
	}
	if opts.dpi < 1 || opts.dpi > 1200 {
		return errors.New("dpi 必须在 1-1200 之间")
	}
	if format == "pdf" {
		return convertToPDF(opts, format)
	}
	if format == "txt" {
		return convertToText(opts)
	}
	return convertToImage(opts, format)
}

func formatFromExtension(output string) string {
	switch strings.ToLower(filepath.Ext(output)) {
	case ".pdf":
		return "pdf"
	case ".txt":
		return "txt"
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpg"
	case ".svg":
		return "svg"
	case ".eps":
		return "eps"
	case ".tex":
		return "tex"
	default:
		return "pdf"
	}
}

func convertToText(opts *options) error {
	var output io.Writer = os.Stdout
	if opts.output != "" && opts.output != "-" {
		file, err := os.Create(ensureExtension(opts.output, "txt"))
		if err != nil {
			return err
		}
		defer file.Close()
		output = file
	}
	var option []converter.Option
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	return converter.Text(opts.input, output, option...)
}

func convertToPDF(opts *options, _ string) error {
	var output io.Writer = os.Stdout
	if opts.output != "" && opts.output != "-" {
		file, err := os.Create(opts.output)
		if err != nil {
			return err
		}
		defer file.Close()
		output = file
	}
	return converter.PDF(opts.input, output)
}

func convertToImage(opts *options, format string) error {
	option := []converter.Option{
		converter.DPI(float64(opts.dpi)),
		converter.BgColor(parseBgColor(opts.bg)),
		imageFormatOption(format),
	}
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}

	if opts.page > 0 {
		return convertSinglePage(opts, format, option)
	}
	return convertAllPages(opts, format, option)
}

func convertSinglePage(opts *options, format string, option []converter.Option) error {
	output := opts.output
	if output == "" || output == "-" {
		return converter.Image(opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{Writer: os.Stdout}, nil
		}))...)
	}
	output = ensureExtension(output, format)
	return converter.Image(opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
		return os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}))...)
}

func convertAllPages(opts *options, format string, option []converter.Option) error {
	if opts.output == "" || opts.output == "-" {
		return errors.New("多页输出需要指定输出目录或 .zip 文件 (-o)")
	}
	if opts.dir {
		return convertToDirectory(opts, format, option)
	}
	if strings.EqualFold(filepath.Ext(opts.output), ".zip") {
		return convertToZip(opts, format, option)
	}
	return convertToDirectory(opts, format, option)
}

func convertToZip(opts *options, format string, option []converter.Option) error {
	file, err := os.Create(opts.output)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	err = converter.Image(opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
		entry, err := archive.Create(fmt.Sprintf("page-%04d.%s", page, format))
		if err != nil {
			return nil, err
		}
		return nopWriteCloser{Writer: entry}, nil
	}))...)
	if err != nil {
		archive.Close()
		return err
	}
	return archive.Close()
}

func convertToDirectory(opts *options, format string, option []converter.Option) error {
	if err := os.MkdirAll(opts.output, 0755); err != nil {
		return err
	}
	return converter.Image(opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
		path := filepath.Join(opts.output, fmt.Sprintf("page-%04d.%s", page, format))
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}))...)
}

func imageFormatOption(format string) converter.Option {
	switch format {
	case "png":
		return converter.PNG()
	case "jpg":
		return converter.JPG()
	case "svg":
		return converter.SVG()
	case "eps":
		return converter.EPS()
	case "tex":
		return converter.TeX()
	default:
		return converter.PNG()
	}
}

func parseBgColor(s string) color.Color {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "transparent", "none":
		return color.Transparent
	case "black":
		return color.Black
	default:
		return color.White
	}
}

func ensureExtension(path, format string) string {
	if filepath.Ext(path) != "" {
		return path
	}
	return path + "." + format
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

// Command ofd-compare 以选定基准对比多个 OFD 的包内文件和页面。
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/compare"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runContext(ctx, args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	options := compare.Options{Visual: true, DPI: 72}
	baseline := 1
	output, format := "", ""
	failOnDifference, showVersion := false, false
	externalImages := false
	executed := false
	cmd := &cobra.Command{
		Use:           "ofd-compare [flags] baseline.ofd other.ofd [more.ofd...]",
		Short:         "对比多份 OFD 的包内文件、XML、文字和页面画面",
		SilenceErrors: true, SilenceUsage: true,
		Args: func(_ *cobra.Command, paths []string) error {
			if showVersion {
				return nil
			}
			if len(paths) < 2 {
				return errors.New("至少需要两份 OFD 文件")
			}
			return nil
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	flags := cmd.Flags()
	flags.StringVarP(&output, "output", "o", "", "报告路径；- 或省略写标准输出")
	flags.StringVar(&format, "format", "", "报告格式：text、markdown、json、html；默认按输出扩展名推断")
	flags.StringVar(&options.Language, "lang", "zh-CN", "Report language: zh-CN or en")
	flags.IntVar(&baseline, "baseline", 1, "基准文件在位置参数中的序号，从 1 开始")
	flags.StringVar(&options.PageMatch, "page-match", "auto", "页面匹配：auto 或 number")
	flags.StringVar(&options.PageMap, "page-map", "", "手工页映射，例如 2:3,4:5；基准页码:各目标页码，仅适用于 auto")
	flags.StringVar(&options.Pages, "pages", "", "各输入参与比较的全局页码，例如 1-3,5；从 1 开始")
	flags.BoolVar(&options.Visual, "visual", true, "比较页面画面；--visual=false 仅比较结构与文字")
	flags.Float64Var(&options.DPI, "dpi", 72, "视觉对比分辨率，10–300")
	flags.IntVar(&options.PixelTolerance, "pixel-tolerance", 0, "每个颜色通道允许的像素差值，0–255")
	flags.IntVar(&options.MinRegionPixels, "min-region-pixels", 1, "视觉差异区域至少包含的变化像素数；不改变原始差异统计")
	flags.Int64Var(&options.MaxXMLBytes, "max-xml-bytes", 8<<20, "每个 XML 结构分析的最大字节数")
	flags.Int64Var(&options.MaxXMLStructureBytes, "max-xml-structure-bytes", 32<<20, "每个 XML 字段表的最大估算字节数")
	flags.IntVar(&options.MaxChanges, "max-changes", 200, "每个 XML 最多展示的差异条数")
	flags.Int64Var(&options.MaxPagePixels, "max-page-pixels", 10_000_000, "单页视觉对比的最大像素数")
	flags.Int64Var(&options.MaxTotalPixels, "max-total-pixels", 1_000_000_000, "所有页面渲染累计像素上限")
	flags.Int64Var(&options.MaxPreviewBytes, "max-preview-bytes", 64<<20, "所有预览图累计编码后的 PNG 字节上限")
	flags.Int64Var(&options.MaxReportBytes, "max-report-bytes", 256<<20, "保留的报告数据和最终输出的字节上限")
	flags.Int64Var(&options.BaselineCacheBytes, "baseline-cache-bytes", 64<<20, "基准页 RGBA 缓存最大字节数；-1 禁用，缓存命中不重复扣像素预算")
	flags.BoolVar(&externalImages, "external-images", false, "将 HTML 图片写入报告旁的独立目录，须指定 HTML 输出文件")
	flags.BoolVar(&failOnDifference, "fail-on-difference", false, "检测到差异时返回退出码 1")
	flags.BoolVarP(&showVersion, "version", "v", false, "输出工具版本")
	usageErr := false
	cmd.RunE = func(_ *cobra.Command, paths []string) error {
		executed = true
		if showVersion {
			_, err := fmt.Fprintln(stdout, version.Version)
			return err
		}
		usageErr = true
		if baseline < 1 || baseline > len(paths) {
			return errors.New("基准文件序号超出范围")
		}
		options.Baseline = baseline - 1
		if format == "" {
			switch strings.ToLower(filepath.Ext(output)) {
			case ".html", ".htm":
				format = "html"
			case ".json":
				format = "json"
			case ".md", ".markdown":
				format = "markdown"
			default:
				format = "text"
			}
		}
		format = strings.ToLower(strings.TrimSpace(format))
		switch format {
		case "text", "markdown", "json", "html":
		default:
			return fmt.Errorf("不支持的报告格式 %q", format)
		}
		if externalImages && (format != "html" || output == "" || output == "-") {
			return errors.New("外置图片需要指定 HTML 输出文件")
		}
		if output != "" && output != "-" {
			for _, input := range paths {
				if utils.SamePath(input, output) {
					return errors.New("报告输出不能覆盖输入 OFD")
				}
				a, ae := os.Stat(input)
				b, be := os.Stat(output)
				if ae == nil && be == nil && os.SameFile(a, b) {
					return errors.New("报告输出不能覆盖输入 OFD 的硬链接")
				}
			}
		}
		// 打开文件前先校验数值选项。
		if options.DPI < 10 || options.DPI > 300 || options.DPI != options.DPI || options.PixelTolerance < 0 || options.PixelTolerance > 255 || options.MaxXMLBytes <= 0 || options.MaxXMLBytes >= math.MaxInt64 || options.MaxChanges <= 0 || options.MaxPagePixels <= 0 || options.MaxTotalPixels <= 0 || options.MaxXMLStructureBytes <= 0 || options.MaxPreviewBytes <= 0 || options.MaxReportBytes <= 0 {
			return errors.New("DPI、容差或资源限制无效")
		}
		if options.PageMatch != "auto" && options.PageMatch != "number" {
			return errors.New("页面匹配方式必须为 auto 或 number")
		}
		if _, err := compare.ParsePageMap(options.PageMap); err != nil {
			return err
		}
		if options.PageMap != "" && options.PageMatch != "auto" {
			return errors.New("手工页映射仅适用于 auto 匹配")
		}
		if _, err := compare.ParsePageRanges(options.Pages); err != nil {
			return err
		}
		if options.MinRegionPixels < 1 {
			return errors.New("区域最小像素数必须为正数")
		}
		if options.BaselineCacheBytes < -1 {
			return errors.New("基准页缓存大小必须为正数或 -1（禁用）")
		}
		usageErr = false
		imagesDir := ""
		keepImages := false
		imageCount := 0
		if externalImages {
			var err error
			imagesDir, err = os.MkdirTemp(filepath.Dir(output), "ofd-compare-images-")
			if err != nil {
				return err
			}
			defer func() {
				if !keepImages {
					_ = os.RemoveAll(imagesDir)
				}
			}()
			written := map[string]bool{}
			options.ImageWriter = func(data []byte) (string, error) {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				name := fmt.Sprintf("%x.png", sha256.Sum256(data))
				if !written[name] {
					if err := os.WriteFile(filepath.Join(imagesDir, name), data, 0600); err != nil {
						return "", err
					}
					written[name] = true
					imageCount++
				}
				return filepath.Base(imagesDir) + "/" + name, nil
			}
		}
		report, err := compare.CompareFiles(ctx, paths, options)
		if err != nil {
			return err
		}
		if output == "" || output == "-" {
			err = compare.WriteReport(stdout, report, format)
		} else {
			err = writeFile(ctx, output, report, format)
		}
		if err != nil {
			return err
		}
		keepImages = imageCount > 0
		if failOnDifference {
			for _, c := range report.Comparisons {
				if !c.Equal {
					return errDifferent
				}
			}
		}
		return nil
	}
	err := cmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, errDifferent) {
		return 1
	}
	_, _ = fmt.Fprintln(stderr, "ofd-compare:", err)
	if !executed || usageErr {
		return 2
	}
	return 3
}

var errDifferent = errors.New("检测到差异")

func writeFile(ctx context.Context, filename string, report *compare.Report, format string) (err error) {
	f, err := os.CreateTemp(filepath.Dir(filename), ".ofd-compare-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if err = compare.WriteReport(f, report, format); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

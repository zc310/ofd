package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/replace"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/watermark"
)

type watermarkOptions struct {
	action string

	input  string
	output string

	compression      string
	compressionLevel int
	signatures       string
	maxEntries       int
	maxEntryMB       int
	maxTotalMB       int
	deterministic    bool
	validate         bool
	verifySignatures bool
	signFlags

	document             int
	pages                []int
	matchIDs             []string
	skipPermissionsCheck bool
	skipReadonlyCheck    bool

	id         uint64
	creator    string
	subtype    string
	visible    bool
	print      bool
	noZoom     bool
	noRotate   bool
	readOnly   bool
	remark     string
	parameters []string

	appearanceFile string
	text           string
	font           string
	fontSize       float64
	color          string
	textX          float64
	textY          float64
	boundary       string
	textObjectID   uint64

	image      string
	imageWidth float64
	opacity    int
	layout     string
	rotate     float64

	fontSet bool

	help bool
}

func parseWatermarkArgs(args []string, output io.Writer) (*watermarkOptions, error) {
	opts := &watermarkOptions{
		compression:  string(creator.CompressionAuto),
		signatures:   string(creator.SignatureDrop),
		document:     -1,
		visible:      true,
		print:        true,
		font:         "",
		fontSize:     9,
		color:        "170 160 165",
		boundary:     "0 0 210 297",
		textObjectID: 1,
		imageWidth:   40,
		layout:       "tile",
	}
	if len(args) == 0 {
		opts.action = ""
	} else {
		opts.action = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}
	root := &cobra.Command{
		Use:           "ofd-creator watermark <add|replace|remove>",
		Short:         "添加、替换或删除 OFD 文档水印",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, positional []string) error {
			if opts.help {
				return nil
			}
			if len(positional) > 0 && opts.input == "" {
				opts.input = positional[0]
			}
			opts.fontSet = cmd.Flags().Changed("font")
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		if opts.action == "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator watermark - 添加、替换或删除 OFD 文档水印")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator watermark <add|replace|remove> -i in.ofd -o out.ofd [选项]")
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		} else {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator watermark "+opts.action)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator watermark "+opts.action+" -i in.ofd -o out.ofd [选项]")
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		}
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "输入的 OFD 文件；也可作为位置参数")
	flags.StringVarP(&opts.output, "output", "o", "", "输出 OFD 路径；使用 - 写入标准输出")
	flags.IntVar(&opts.document, "document", opts.document, "目标文档体下标，-1 表示全部文档体")
	flags.IntSliceVar(&opts.pages, "page", nil, "目标页面的下标（从 0 开始），可重复；省略表示全部页面")
	flags.StringSliceVar(&opts.matchIDs, "match-id", nil, "仅替换/删除匹配的注解 ID，可重复；省略表示全部水印")
	flags.StringVar(&opts.compression, "compression", opts.compression, "ZIP 压缩策略：auto、deflate 或 store")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "DEFLATE 压缩级别：0 使用默认级别 5，1（最快）到 9（最紧凑）")
	flags.StringVar(&opts.signatures, "signatures", opts.signatures, "签名处理方式：drop、preserve 或 rewrite")
	flags.IntVar(&opts.maxEntries, "max-entries", 0, "最多搬运的条目数，0 表示使用默认值 10000")
	flags.IntVar(&opts.maxEntryMB, "max-entry-mb", 0, "单个条目解压后的最大 MB，0 表示使用默认值 64")
	flags.IntVar(&opts.maxTotalMB, "max-total-mb", 0, "所有条目解压后的总 MB 上限，0 表示使用默认值 512")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间，生成可复现的 OFD")
	flags.BoolVar(&opts.validate, "validate", false, "修改后对整体输出执行严格 OFD 校验")
	flags.BoolVar(&opts.verifySignatures, "verify-signatures", false, "修改后校验输出文档的签名摘要与密码学签名")
	registerSignFlags(flags, &opts.signFlags)
	flags.BoolVar(&opts.skipPermissionsCheck, "skip-permissions-check", false, "跳过文档级水印权限检查（Permissions/Watermark=false 时默认拒绝）")
	flags.BoolVar(&opts.skipReadonlyCheck, "skip-readonly-check", false, "跳过只读检查（ReadOnly 为 true 的水印默认拒绝替换/删除）")
	flags.Uint64Var(&opts.id, "id", 0, "水印注解 ID；0 表示自动分配")
	flags.StringVar(&opts.creator, "creator", "", "水印创建者名称，默认 ofd-creator")
	flags.StringVar(&opts.subtype, "subtype", "", "水印注解子类型")
	flags.BoolVar(&opts.visible, "visible", opts.visible, "水印注解是否可见")
	flags.BoolVar(&opts.print, "print", opts.print, "水印注解是否随文档打印")
	flags.BoolVar(&opts.noZoom, "no-zoom", false, "水印注解不随页面缩放")
	flags.BoolVar(&opts.noRotate, "no-rotate", false, "水印注解不随页面旋转")
	flags.BoolVar(&opts.readOnly, "read-only", false, "水印注解为只读（默认写出 false，替换/删除时不需跳过只读检查）")
	flags.StringVar(&opts.remark, "remark", "", "水印注解备注")
	flags.StringSliceVar(&opts.parameters, "parameter", nil, "自定义参数，格式 Name=Value，可重复")
	flags.StringVar(&opts.appearanceFile, "appearance", "", "外观 XML 片段文件；与 --text 互斥")
	flags.StringVar(&opts.text, "text", "", "水印文字内容，用于生成简化外观；与 --appearance 互斥")
	flags.StringVar(&opts.font, "font", opts.font, "生成外观使用的字体 ID 或名称（须在文档资源中存在）")
	flags.Float64Var(&opts.fontSize, "font-size", opts.fontSize, "生成外观使用的字号")
	flags.StringVar(&opts.color, "color", opts.color, "生成外观使用的填充颜色，格式 \"R G B\"")
	flags.Float64Var(&opts.textX, "text-x", opts.textX, "水印文字起始 X（平铺/居中时作为整体偏移）")
	flags.Float64Var(&opts.textY, "text-y", opts.textY, "水印文字起始 Y（平铺/居中时作为整体偏移）")
	flags.StringVar(&opts.boundary, "boundary", opts.boundary, "生成外观的边界与布局区域，格式 \"X Y Width Height\"")
	flags.Uint64Var(&opts.textObjectID, "text-object-id", opts.textObjectID, "生成外观中文本对象的 ID")
	flags.StringVar(&opts.image, "image", "", "图片水印文件（PNG/JPEG，PNG 支持 --opacity）；与 --text/--appearance 互斥")
	flags.Float64Var(&opts.imageWidth, "image-width", opts.imageWidth, "图片水印单张显示宽度（毫米），高度按图片比例推算")
	flags.IntVar(&opts.opacity, "opacity", 0, "水印不透明度：0 到 100，0 表示不设置（文字写 Alpha，PNG 图片烘焙进 alpha 通道）")
	flags.StringVar(&opts.layout, "layout", opts.layout, "水印布局方式：tile（平铺）或 center（居中）")
	flags.Float64Var(&opts.rotate, "rotate", 0, "水印文字旋转角度（度），正值屏幕顺时针（左边往上、右边向下），绕文本中心倾斜；0 表示不旋转")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	return opts, nil
}

func validateWatermarkOptions(opts *watermarkOptions) error {
	switch opts.action {
	case "add", "replace", "remove":
	default:
		return fmt.Errorf("未知的水印操作 %q，应为 add、replace 或 remove", opts.action)
	}
	if strings.TrimSpace(opts.input) == "" {
		return errors.New("缺少输入的 OFD 文件")
	}
	if strings.TrimSpace(opts.output) == "" {
		return errors.New("缺少输出 OFD 文件")
	}
	if opts.input == "-" {
		return errors.New("watermark 不支持从标准输入读取 OFD")
	}
	if opts.output != "-" && utils.SamePath(opts.input, opts.output) {
		return errors.New("输出文件不能覆盖输入 OFD 文件")
	}
	switch creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))) {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return fmt.Errorf("不支持的 ZIP 压缩策略 %q", opts.compression)
	}
	if _, err := creator.NormalizeCompressionLevel(opts.compressionLevel); err != nil {
		return err
	}
	switch creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))) {
	case "", creator.SignatureDrop, creator.SignaturePreserve, creator.SignatureRewrite:
	default:
		return fmt.Errorf("不支持的签名处理方式 %q", opts.signatures)
	}
	if opts.maxEntries < 0 || opts.maxEntryMB < 0 || opts.maxTotalMB < 0 {
		return errors.New("规模限制不能为负数")
	}
	if strings.TrimSpace(opts.appearanceFile) != "" && strings.TrimSpace(opts.text) != "" {
		return errors.New("--appearance 与 --text 互斥")
	}
	provided := 0
	if strings.TrimSpace(opts.appearanceFile) != "" {
		provided++
	}
	if strings.TrimSpace(opts.text) != "" {
		provided++
	}
	if strings.TrimSpace(opts.image) != "" {
		provided++
	}
	if provided > 1 {
		return errors.New("--text、--appearance 与 --image 只能选择其一")
	}
	switch strings.ToLower(strings.TrimSpace(opts.layout)) {
	case "tile", "center":
	default:
		return fmt.Errorf("不支持的布局方式 %q，应为 tile 或 center", opts.layout)
	}
	if opts.opacity < 0 || opts.opacity > 100 {
		return errors.New("--opacity 取值范围为 0 到 100")
	}
	if opts.rotate != 0 && strings.TrimSpace(opts.text) == "" {
		return errors.New("--rotate 仅对 --text 文字水印生效")
	}
	if opts.imageWidth <= 0 {
		return errors.New("--image-width 必须为正数")
	}
	if strings.TrimSpace(opts.text) != "" {
		// 文字水印的 Font 必须是文档中存在的字体，解析为数值 ID，避免写出非法引用。
		resolved, err := resolveWatermarkFont(opts.input, opts.document, opts.font)
		if err != nil {
			return err
		}
		opts.font = resolved
		opts.fontSet = true
	}
	for _, parameter := range opts.parameters {
		if !strings.Contains(parameter, "=") || strings.TrimSpace(strings.SplitN(parameter, "=", 2)[0]) == "" {
			return fmt.Errorf("--parameter 需为 Name=Value 格式: %q", parameter)
		}
	}
	return nil
}

func runWatermark(args []string, stdout, stderr io.Writer) int {
	opts, err := parseWatermarkArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateWatermarkOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
		return exitUsage
	}

	wm, appearance, err := buildWatermarkValues(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
		return exitUsage
	}
	if opts.action == "add" || opts.action == "replace" {
		wm.Appearance = appearance
	}

	target := watermark.Target{Document: opts.document, Pages: opts.pages}
	if opts.action == "replace" || opts.action == "remove" {
		ids, parseErr := parseMatchIDs(opts.matchIDs)
		if parseErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", parseErr)
			return exitUsage
		}
		target.MatchIDs = ids
	}

	var buffer bytes.Buffer
	replacementOptions := replacementOptionsFor(opts)
	watermarkOptions := watermark.Options{
		Options:              replacementOptions,
		SkipPermissionsCheck: opts.skipPermissionsCheck,
		SkipReadOnlyCheck:    opts.skipReadonlyCheck,
	}
	switch opts.action {
	case "add":
		err = watermark.Add(opts.input, target, wm, &buffer, watermarkOptions)
	case "replace":
		err = watermark.Replace(opts.input, target, wm, &buffer, watermarkOptions)
	case "remove":
		err = watermark.Remove(opts.input, target, &buffer, watermarkOptions)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
		return exitResource
	}
	data := buffer.Bytes()
	if strings.TrimSpace(opts.signCmd) != "" {
		var signed bytes.Buffer
		if err := sign.Sign(data, &signed, buildSignOptions(&opts.signFlags, opts.deterministic)); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
			return exitResource
		}
		data = signed.Bytes()
	}
	if opts.verifySignatures {
		statuses, verifyErr := merge.VerifySignatures(data)
		if verifyErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", verifyErr)
			return exitResource
		}
		reportSignatureVerification(stderr, "ofd-creator watermark", statuses)
	}
	if err := writeOutput(opts.output, data, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator watermark:", err)
		return exitOutput
	}
	return exitOK
}

func replacementOptionsFor(opts *watermarkOptions) replace.Options {
	return replace.Options{
		Compression:      creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))),
		CompressionLevel: opts.compressionLevel,
		Deterministic:    opts.deterministic,
		Signatures:       creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))),
		Limits: replace.Limits{
			MaxEntries:    opts.maxEntries,
			MaxEntryBytes: int64(opts.maxEntryMB) << 20,
			MaxTotalBytes: int64(opts.maxTotalMB) << 20,
		},
		Validate: opts.validate,
		OnWarning: func(message string) {
			_, _ = fmt.Fprintln(os.Stderr, "ofd-creator watermark: 警告:", message)
		},
	}
}

func buildWatermarkValues(opts *watermarkOptions) (wm watermark.Watermark, appearance []byte, err error) {
	boundary, parseErr := parseBox(opts.boundary)
	if parseErr != nil {
		return wm, nil, parseErr
	}
	wm = watermark.Watermark{
		ID:       opts.id,
		Creator:  opts.creator,
		Subtype:  opts.subtype,
		NoZoom:   opts.noZoom,
		NoRotate: opts.noRotate,
		ReadOnly: &opts.readOnly,
		Remark:   opts.remark,
		Boundary: &boundary,
	}
	// 显式写出与 XSD 默认不同的取值：Visible 默认 true、Print 默认 true。
	// Visible 始终写出，保证 --visible=false 生效（省略会被按默认 true 解释）。
	wm.Visible = &opts.visible
	if !opts.print {
		wm.Print = &opts.print
	}
	for _, parameter := range opts.parameters {
		parts := strings.SplitN(parameter, "=", 2)
		wm.Parameters = append(wm.Parameters, creator.AnnotationParameter{Name: strings.TrimSpace(parts[0]), Value: parts[1]})
	}
	if strings.TrimSpace(opts.text) != "" {
		appearance, err = buildTextAppearance(opts, boundary)
		if err != nil {
			return wm, nil, err
		}
		return wm, appearance, nil
	}
	if strings.TrimSpace(opts.image) != "" {
		data, readErr := os.ReadFile(strings.TrimSpace(opts.image))
		if readErr != nil {
			return wm, nil, fmt.Errorf("读取图片文件失败: %w", readErr)
		}
		wm.Image = &watermark.Image{
			Data:   data,
			Width:  opts.imageWidth,
			Layout: layoutFor(opts.layout),
		}
		if opts.opacity > 0 {
			value := uint8(opts.opacity * 255 / 100)
			wm.Image.Opacity = &value
		}
		return wm, nil, nil
	}
	if strings.TrimSpace(opts.appearanceFile) != "" {
		data, readErr := os.ReadFile(opts.appearanceFile)
		if readErr != nil {
			return wm, nil, fmt.Errorf("读取外观文件失败: %w", readErr)
		}
		return wm, data, nil
	}
	return wm, nil, nil
}

// layoutFor 把 CLI 布局名转换为库布局常量。
func layoutFor(name string) watermark.TextLayout {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "center":
		return watermark.LayoutCenter
	default:
		return watermark.LayoutTile
	}
}

func buildTextAppearance(opts *watermarkOptions, boundary creator.Box) ([]byte, error) {
	color, err := parseColor(opts.color)
	if err != nil {
		return nil, err
	}
	fill := true
	return watermark.TextAppearance(watermark.TextOptions{
		ID:       opts.textObjectID,
		Font:     opts.font,
		Size:     opts.fontSize,
		Text:     opts.text,
		X:        opts.textX,
		Y:        opts.textY,
		Boundary: boundary,
		Color:    color,
		Fill:     &fill,
		Layout:   layoutFor(opts.layout),
		Opacity:  opacityFor(opts.opacity),
		Rotation: opts.rotate,
	})
}

// opacityFor 把 CLI 的 0-100 不透明度转换为 Alpha 属性值；0 表示不设置。
func opacityFor(value int) *uint8 {
	if value <= 0 {
		return nil
	}
	alpha := uint8(value * 255 / 100)
	return &alpha
}

func parseColor(text string) (*creator.Color, error) {
	parts := strings.Fields(text)
	if len(parts) != 3 {
		return nil, fmt.Errorf("颜色格式应为 \"R G B\": %q", text)
	}
	values := make([]int, 3)
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return nil, fmt.Errorf("无效的颜色分量 %q", part)
		}
		values[index] = value
	}
	return &creator.Color{R: uint8(values[0]), G: uint8(values[1]), B: uint8(values[2])}, nil
}

func parseBox(text string) (creator.Box, error) {
	parts := strings.Fields(text)
	if len(parts) != 4 {
		return creator.Box{}, fmt.Errorf("边界格式应为 \"X Y Width Height\": %q", text)
	}
	values := make([]float64, 4)
	for index, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return creator.Box{}, fmt.Errorf("无效的边界分量 %q", part)
		}
		values[index] = value
	}
	return creator.Box{X: values[0], Y: values[1], Width: values[2], Height: values[3]}, nil
}

func parseMatchIDs(specs []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(specs))
	for _, spec := range specs {
		id, err := strconv.ParseUint(strings.TrimSpace(spec), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("无效的注解 ID %q", spec)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveWatermarkFont 在目标文档体中解析 --font 指定的字体 ID 或名称，
// 返回可直接写入外观的数值字体 ID。同一文档体的 PublicRes 与 DocumentRes 均参与匹配。
func resolveWatermarkFont(input string, docIndex int, want string) (string, error) {
	want = strings.TrimSpace(want)
	pkg, err := core.OpenFile(input)
	if err != nil {
		return "", fmt.Errorf("打开 %s 校验字体失败: %w", input, err)
	}
	defer func() { _ = pkg.Close() }()
	var root models.OFD
	if err := pkg.ReadXML(spec.RootDocument, &root); err != nil {
		return "", fmt.Errorf("读取 %s 失败: %w", spec.RootDocument, err)
	}
	indexes := make([]int, 0, len(root.DocBodies))
	if docIndex < 0 {
		for i := range root.DocBodies {
			indexes = append(indexes, i)
		}
	} else {
		indexes = append(indexes, docIndex)
	}
	byID := make(map[string]bool)
	byName := make(map[string]string)
	for _, bodyIndex := range indexes {
		if bodyIndex < 0 || bodyIndex >= len(root.DocBodies) {
			return "", fmt.Errorf("文档体下标 %d 超出范围（共 %d 个）", bodyIndex, len(root.DocBodies))
		}
		docEntry := strings.TrimLeft(root.DocBodies[bodyIndex].DocRoot.Resolve("/").String(), "/")
		baseLoc := models.StLoc(path.Dir(docEntry))
		var document models.Document
		if err := pkg.ReadXML(docEntry, &document); err != nil {
			return "", fmt.Errorf("读取 %s 失败: %w", docEntry, err)
		}
		// PublicRes 与 DocumentRes 中的字体属于同一文档体，均需纳入。
		resLocations := make([]models.StLoc, 0, len(document.CommonData.PublicRes)+len(document.CommonData.DocumentRes))
		resLocations = append(resLocations, document.CommonData.PublicRes...)
		resLocations = append(resLocations, document.CommonData.DocumentRes...)
		for _, loc := range resLocations {
			resEntry := strings.TrimLeft(loc.Resolve(baseLoc).String(), "/")
			var resources models.Res
			if err := pkg.ReadXML(resEntry, &resources); err != nil {
				continue
			}
			if resources.Fonts == nil {
				continue
			}
			for _, font := range resources.Fonts.Font {
				id := strconv.FormatUint(uint64(font.ID), 10)
				byID[id] = true
				for _, name := range []string{font.FontName, font.FamilyName} {
					if name != "" {
						if _, exists := byName[name]; !exists {
							byName[name] = id
						}
					}
				}
			}
		}
	}
	if want == "" {
		// 未指定字体时使用文档中最小的字体 ID，避免写出非法引用。
		best := ""
		for id := range byID {
			if best == "" || compareNumericID(id, best) < 0 {
				best = id
			}
		}
		if best == "" {
			return "", errors.New("文档没有可用字体资源，无法为文字水印选择字体，请用 --font 指定")
		}
		return best, nil
	}
	if byID[want] {
		return want, nil
	}
	if id, ok := byName[want]; ok {
		return id, nil
	}
	if len(byID) == 0 {
		return "", fmt.Errorf("文档没有可用字体资源，无法解析 --font %q", want)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("字体 %q 不存在于文档资源（可用：%s）", want, strings.Join(names, "、"))
}

// compareNumericID 比较两个数值字符串 ID 的大小，非数值回退为字典序。
func compareNumericID(a, b string) int {
	ai, aerr := strconv.ParseUint(a, 10, 64)
	bi, berr := strconv.ParseUint(b, 10, 64)
	if aerr == nil && berr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

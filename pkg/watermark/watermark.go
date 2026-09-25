// Package watermark 提供 OFD 水印注解的添加、替换与删除能力。
//
// 水印以页面注解文件（<PageAnnot>）中的 Type="Watermark" 注解表示。本包
// 复用 pkg/replace 的 ZIP 重建框架：先按包内结构定位目标页面与注解文件，
// 计算出一组条目操作后交给 replace.Files 执行。
package watermark

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
)

// Target 描述水印操作的目标范围。
type Target struct {
	// Document 是文档体下标，-1 表示全部文档体，0 表示第一个文档体。
	Document int
	// Pages 是页面下标列表，相对于 Document.xml 的 <Pages> 顺序；空表示全部页面。
	Pages []int
	// MatchIDs 是仅匹配的注解 ID；Replace/Remove 使用，空表示匹配全部水印注解。
	MatchIDs []uint64
}

// Watermark 描述一条水印注解及其外观。
type Watermark struct {
	// ID 是注解 ID，0 表示自动分配（取文档最大对象标识与页面内既有注解 ID 之后的空闲值）。
	ID uint64
	// Creator 是创建者名称，空值使用默认名称。
	Creator string
	// LastModDate 是最后修改日期，零值使用当天。
	LastModDate time.Time
	// Visible 指定注解是否可见；空值按 XSD 默认 true。
	Visible *bool
	// Subtype 是注解子类型。
	Subtype string
	// Print 指定是否随文档打印；空值按 XSD 默认 true。
	Print *bool
	// NoZoom 指定注解不随页面缩放。
	NoZoom bool
	// NoRotate 指定注解不随页面旋转。
	NoRotate bool
	// ReadOnly 指定注解为只读；空值按 XSD 默认 true。
	ReadOnly *bool
	// Remark 是注解备注。
	Remark string
	// Parameters 是注解自定义参数。
	Parameters []creator.AnnotationParameter
	// Boundary 是注解外观边界（毫米），为空时 `<Appearance>` 不写 Boundary 属性。
	Boundary *creator.Box
	// Appearance 是 `<Appearance>` 内容（页面对象片段）的原始 XML；为空时外观为空元素。
	// 设置了 Image 时本字段被忽略，外观由引擎根据图片资源自动生成。
	Appearance []byte
	// Image 指定图片水印：引擎会把图片写入文档资源、注册到 DocumentRes.xml，
	// 并按布局方式生成 ImageObject 外观。设置后忽略 Appearance。
	Image *Image
}

// Image 描述一条图片水印及其嵌入方式。
type Image struct {
	// Data 是图片字节（如 PNG）。设置了 Opacity 时仅支持 PNG，并按不透明度
	// 烘焙进图片自身的 alpha 通道后嵌入。
	Data []byte
	// Format 是 DocumentRes.xml 的 Format 属性值（如 "PNG"、"JPEG"），空值默认 "PNG"。
	Format string
	// Width 是单张图片显示宽度（毫米），0 使用默认值 40。
	Width float64
	// Height 是单张图片显示高度（毫米），0 时按图片像素宽高比推算。
	Height float64
	// Layout 指定水印在页面上的布局方式，默认 LayoutManual。
	Layout TextLayout
	// Opacity 是整体不透明度（0 到 255），设置后烘焙进 PNG 的 alpha 通道。
	Opacity *uint8
}

// Options 控制水印编辑行为。
type Options struct {
	replace.Options
	// SkipPermissionsCheck 跳过文档级水印权限检查（Permissions/Watermark=false 时默认拒绝）。
	SkipPermissionsCheck bool
	// SkipReadOnlyCheck 跳过只读检查（ReadOnly 缺省或为 true 的水印默认拒绝 Replace/Remove）。
	SkipReadOnlyCheck bool
}

// Add 在 target 覆盖的每个页面各添加一条水印注解。
func Add(input any, target Target, wm Watermark, w io.Writer, options Options) error {
	return run(input, target, wm, actionAdd, w, options)
}

// Replace 替换 target 内匹配的水印注解内容（保留其 ID）。
func Replace(input any, target Target, wm Watermark, w io.Writer, options Options) error {
	return run(input, target, wm, actionReplace, w, options)
}

// Remove 删除 target 内匹配的水印注解；清理产生的空注解文件与索引。
func Remove(input any, target Target, w io.Writer, options Options) error {
	return run(input, target, Watermark{}, actionRemove, w, options)
}

func run(input any, target Target, wm Watermark, action actionKind, w io.Writer, options Options) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	engine := &engine{options: options}
	if err := engine.run(input, target, wm, action); err != nil {
		return err
	}
	operations, err := engine.operations()
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return errors.New("水印操作未产生任何修改")
	}
	return replace.Files(engine.filesInput, operations, w, options.Options)
}

func defaultCreator() string { return "ofd-creator" }

const watermarkType = "Watermark"

func watermarkExistsError() error { return errors.New("未找到匹配的水印") }
func docBodyOutOfRange(index, count int) error {
	return fmt.Errorf("文档体下标 %d 超出范围（共 %d 个）", index, count)
}

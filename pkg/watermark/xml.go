package watermark

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
)

// numberPrecision 与 pkg/creator 保持一致：0.0001mm 足够、避免浮点噪声。
const numberPrecision = 4

// maxInt64Float 是可安全转换到 int64 的最大 float64（math.MaxInt64 的浮点值）。
const maxInt64Float = float64(math.MaxInt64)

// fmtNumber 以固定小数位输出浮点数，去掉末尾多余的 0 和小数点，不使用科学计数法。
func fmtNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}
	if value == math.Trunc(value) && value >= -maxInt64Float && value <= maxInt64Float {
		return strconv.FormatInt(int64(value), 10)
	}
	buf := strconv.AppendFloat(make([]byte, 0, 24), value, 'f', numberPrecision, 64)
	end := len(buf)
	for end > 1 && buf[end-1] == '0' {
		end--
	}
	if end > 1 && buf[end-1] == '.' {
		end--
	}
	out := string(buf[:end])
	if out == "-0" {
		return "0"
	}
	return out
}

// fmtBox 输出 "x y width height" 四参数边界。
func fmtBox(x, y, width, height float64) string {
	return fmtNumber(x) + " " + fmtNumber(y) + " " + fmtNumber(width) + " " + fmtNumber(height)
}

// buildAnnotElement 按 creator 属性顺序构造一条水印注解。
func buildAnnotElement(prefix string, wm Watermark, id uint64) (*etree.Element, error) {
	element := etree.NewElement(prefixTag(prefix, "Annot"))
	element.CreateAttr("ID", strconv.FormatUint(id, 10))
	element.CreateAttr("Type", watermarkType)
	creatorName := wm.Creator
	if creatorName == "" {
		creatorName = defaultCreator()
	}
	element.CreateAttr("Creator", creatorName)
	date := wm.LastModDate
	if date.IsZero() {
		date = time.Now()
	}
	element.CreateAttr("LastModDate", date.Format("2006-01-02"))
	if wm.Visible != nil {
		element.CreateAttr("Visible", strconv.FormatBool(*wm.Visible))
	}
	if wm.Subtype != "" {
		element.CreateAttr("Subtype", wm.Subtype)
	}
	if wm.Print != nil {
		element.CreateAttr("Print", strconv.FormatBool(*wm.Print))
	}
	if wm.NoZoom {
		element.CreateAttr("NoZoom", "true")
	}
	if wm.NoRotate {
		element.CreateAttr("NoRotate", "true")
	}
	if wm.ReadOnly != nil {
		element.CreateAttr("ReadOnly", strconv.FormatBool(*wm.ReadOnly))
	}
	if wm.Remark != "" {
		element.CreateElement(prefixTag(prefix, "Remark")).SetText(wm.Remark)
	}
	if len(wm.Parameters) > 0 {
		parameters := element.CreateElement(prefixTag(prefix, "Parameters"))
		for _, parameter := range wm.Parameters {
			item := parameters.CreateElement(prefixTag(prefix, "Parameter"))
			item.CreateAttr("Name", parameter.Name)
			item.SetText(parameter.Value)
		}
	}
	appearance := element.CreateElement(prefixTag(prefix, "Appearance"))
	if wm.Boundary != nil {
		appearance.CreateAttr("Boundary", fmtBox(wm.Boundary.X, wm.Boundary.Y, wm.Boundary.Width, wm.Boundary.Height))
	}
	if len(wm.Appearance) > 0 {
		if err := appendRawXML(appearance, wm.Appearance, prefix); err != nil {
			return nil, fmt.Errorf("解析水印外观失败: %w", err)
		}
	}
	return element, nil
}

// appendRawXML 把 XML 片段的内容拷贝到父元素下。为保持文档命名空间风格，
// 片段中未带前缀的元素会被套用父级 prefix（与页面注解文件一致）。
func appendRawXML(parent *etree.Element, data []byte, prefix string) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "<?xml") {
		return fmt.Errorf("XML 片段不能包含 XML 声明")
	}
	document := etree.NewDocument()
	wrapped := append([]byte("<RawXML>"), data...)
	wrapped = append(wrapped, []byte("</RawXML>")...)
	if err := document.ReadFromBytes(wrapped); err != nil {
		return err
	}
	root := document.Root()
	for _, child := range root.Child {
		switch value := child.(type) {
		case *etree.Element:
			copied := value.Copy()
			applyPrefix(copied, prefix, root)
			parent.AddChild(copied)
		case *etree.CharData:
			if value.IsCData() {
				parent.AddChild(etree.NewCData(value.Data))
			} else if value.Data != "" {
				parent.CreateText(value.Data)
			}
		case *etree.Comment:
			parent.CreateComment(value.Data)
		case *etree.Directive:
			parent.CreateDirective(value.Data)
		case *etree.ProcInst:
			parent.CreateProcInst(value.Target, value.Inst)
		}
	}
	return nil
}

// applyPrefix 递归地把片段中未声明命名空间的元素套用 prefix，避免写出空命名空间
// 的 TextObject/ImageObject 等，破坏 XSD 校验。已在片段内绑定到其它前缀的元素不动。
func applyPrefix(el *etree.Element, prefix string, root *etree.Element) {
	if prefix != "" && el.Space == "" && !hasNamespaceBinding(el, root) {
		el.Space = prefix
	}
	for _, child := range el.ChildElements() {
		applyPrefix(child, prefix, root)
	}
}

// hasNamespaceBinding 判断元素自身或祖先是否声明了默认命名空间（xmlns）。
func hasNamespaceBinding(el, root *etree.Element) bool {
	for node := el; node != nil; node = node.Parent() {
		for _, attr := range node.Attr {
			if (attr.Space == "" && attr.Key == "xmlns") || (attr.Space == "xmlns" && attr.Key == "") {
				return true
			}
		}
		if node == root {
			break
		}
	}
	return false
}

// appendAnnotElement 把新的水印注解追加到页注解根元素。
func appendAnnotElement(root *etree.Element, prefix string, wm Watermark, id uint64) error {
	element, err := buildAnnotElement(prefix, wm, id)
	if err != nil {
		return err
	}
	root.AddChild(element)
	return nil
}

// replaceAnnotElement 用新内容替换既有水印注解并保留其 ID。
func replaceAnnotElement(root *etree.Element, child *etree.Element, prefix string, wm Watermark) error {
	index := child.Index()
	id, err := strconv.ParseUint(child.SelectAttrValue("ID", ""), 10, 64)
	if err != nil {
		id = 0
	}
	element, err := buildAnnotElement(prefix, wm, id)
	if err != nil {
		return err
	}
	root.RemoveChild(child)
	root.InsertChildAt(index, element)
	return nil
}

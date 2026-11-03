package pdfcpuwriter

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/render/drawing"
)

var errInvalidDestination = errors.New("PDF 跳转目标无效")

// AddLinks 给当前页添加链接；目标页可以尚未写入，在 Write 时统一解析。
// 链接矩形及目标沿用 drawing 的毫米、左上角坐标约定。
func (r *Document) AddLinks(links []drawing.PageLink) error {
	if !r.ready() {
		return r.Err()
	}
	for _, link := range links {
		if link.Target != nil {
			target := *link.Target
			link.Target = &target
		}
		r.links = append(r.links, link)
	}
	return nil
}

// AddOutlines 按先序顺序追加大纲，Level 表示层级；内部目标与 URI 二选一。
func (r *Document) AddOutlines(outlines []drawing.PageOutline) error {
	if !r.ready() {
		return r.Err()
	}
	for _, outline := range outlines {
		if outline.Target != nil {
			target := *outline.Target
			outline.Target = &target
		}
		r.outlines = append(r.outlines, outline)
	}
	return nil
}

func pdfBytes(value string) types.HexLiteral {
	return types.HexLiteral(hex.EncodeToString([]byte(value)))
}

func pdfTitle(value string) types.HexLiteral {
	units := utf16.Encode([]rune(value))
	data := make([]byte, 2, 2+2*len(units))
	data[0], data[1] = 0xfe, 0xff
	for _, unit := range units {
		data = append(data, byte(unit>>8), byte(unit))
	}
	return pdfBytes(string(data))
}

// destination 遵循 ISO 32000-1 §12.3.2 表151：坐标用点，Zoom 为无量纲倍率。
func (r *Document) destination(target drawing.LinkTarget) (types.Array, error) {
	if target.Page < 0 || target.Page >= len(r.pages) {
		return nil, fmt.Errorf("%w：页序号 %d", errInvalidDestination, target.Page)
	}
	for _, value := range []float64{target.Left, target.Top, target.Right, target.Bottom, target.Zoom} {
		if math.IsNaN(value) || math.IsInf(value*prototypePointsPerMM, 0) {
			return nil, fmt.Errorf("%w：坐标或倍率不是有限数", errInvalidDestination)
		}
	}
	if target.Zoom < 0 {
		return nil, fmt.Errorf("%w：缩放倍率小于零", errInvalidDestination)
	}
	height := r.pageHeights[target.Page]
	pt := func(mm float64) types.Float { return types.Float(mm * prototypePointsPerMM) }
	dest := types.Array{r.pages[target.Page]}
	switch target.Type {
	case drawing.DestFit:
		dest = append(dest, types.Name("Fit"))
	case drawing.DestFitH:
		dest = append(dest, types.Name("FitH"), pt(height-target.Top))
	case drawing.DestFitV:
		dest = append(dest, types.Name("FitV"), pt(target.Left))
	case drawing.DestXYZ:
		dest = append(dest, types.Name("XYZ"), pt(target.Left), pt(height-target.Top), types.Float(target.Zoom))
	case drawing.DestFitR:
		dest = append(dest, types.Name("FitR"), pt(target.Left), pt(height-target.Bottom), pt(target.Right), pt(height-target.Top))
	default:
		return nil, fmt.Errorf("%w：类型 %d", errInvalidDestination, target.Type)
	}
	for _, value := range dest {
		if number, ok := value.(types.Float); ok && (math.IsNaN(float64(number)) || math.IsInf(float64(number), 0)) {
			return nil, fmt.Errorf("%w：坐标换算溢出", errInvalidDestination)
		}
	}
	return dest, nil
}

// writeNavigation 在页面引用齐备后构造名称树、链接注解及大纲树。
// ISO 32000-1 §7.9.6、§12.3.2–12.3.3、§12.5.6.5、§12.6.4.7。
func (r *Document) writeNavigation() error {
	catalog, err := r.pdf.Catalog()
	if err != nil {
		return err
	}
	dests := map[string]types.Array{}
	for i, links := range r.pageLinks {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		annots := types.Array{}
		for _, link := range links {
			if err := r.ctx.Err(); err != nil {
				return err
			}
			x0, y0, x1, y1 := link.Rect(r.pageHeights[i])
			for _, value := range []float64{x0, y0, x1, y1} {
				if math.IsNaN(value) || math.IsInf(value*prototypePointsPerMM, 0) {
					return fmt.Errorf("%w：链接矩形不是有限数", errInvalidDestination)
				}
			}
			annot := types.Dict{
				"Type": types.Name("Annot"), "Subtype": types.Name("Link"),
				"Rect":   types.Array{types.Float(x0 * prototypePointsPerMM), types.Float(y0 * prototypePointsPerMM), types.Float(x1 * prototypePointsPerMM), types.Float(y1 * prototypePointsPerMM)},
				"Border": types.NewIntegerArray(0, 0, 0),
			}
			if link.Target == nil {
				annot["A"] = uriAction(link.URI)
			} else {
				dest, err := r.destination(*link.Target)
				if err != nil {
					return err
				}
				name := link.Target.Name
				if name == "" {
					annot["Dest"] = dest
				} else {
					if previous, ok := dests[name]; ok && !slices.Equal(previous, dest) {
						return fmt.Errorf("%w：重复名称 %q 指向不同目标", errInvalidDestination, name)
					}
					dests[name] = dest
					annot["Dest"] = pdfBytes(name)
				}
			}
			annots = append(annots, r.object(annot))
		}
		if len(annots) != 0 {
			page, err := r.pdf.DereferenceDict(r.pages[i])
			if err != nil {
				return err
			}
			page["Annots"] = annots
		}
	}
	if len(dests) != 0 {
		keys := make([]string, 0, len(dests))
		for key := range dests {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		names := types.Array{}
		for _, key := range keys {
			names = append(names, pdfBytes(key), dests[key])
		}
		catalog["Names"] = types.Dict{"Dests": r.object(types.Dict{"Names": names})}
	}
	return r.writeOutlines(catalog)
}

func uriAction(uri string) types.Dict {
	return types.Dict{"S": types.Name("URI"), "URI": pdfBytes(uri)}
}

type outlineNode struct {
	dict  types.Dict
	ref   types.IndirectRef
	level int
	count int
	last  *outlineNode
}

func (r *Document) writeOutlines(catalog types.Dict) error {
	if len(r.outlines) == 0 {
		return r.err
	}
	root := &outlineNode{dict: types.Dict{"Type": types.Name("Outlines")}, level: -1}
	root.ref = r.object(root.dict)
	stack := []*outlineNode{root}
	// 出栈时累计后代数，避免逐项遍历所有祖先导致深层大纲退化。
	pop := func() {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node.count != 0 {
			node.dict["Count"] = types.Integer(node.count)
		}
		stack[len(stack)-1].count += 1 + node.count
	}
	for _, outline := range r.outlines {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if outline.Target == nil && outline.URI == "" {
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].level >= outline.Level {
			pop()
		}
		parent := stack[len(stack)-1]
		node := &outlineNode{dict: types.Dict{"Title": pdfTitle(outline.Title), "Parent": parent.ref}, level: outline.Level}
		if outline.Target != nil {
			dest, err := r.destination(*outline.Target)
			if err != nil {
				return err
			}
			node.dict["Dest"] = dest
		} else {
			node.dict["A"] = uriAction(outline.URI)
		}
		node.ref = r.object(node.dict)
		if parent.last == nil {
			parent.dict["First"] = node.ref
		} else {
			node.dict["Prev"] = parent.last.ref
			parent.last.dict["Next"] = node.ref
		}
		parent.last = node
		parent.dict["Last"] = node.ref
		stack = append(stack, node)
	}
	for len(stack) > 1 {
		pop()
	}
	if root.count != 0 {
		root.dict["Count"] = types.Integer(root.count)
		catalog["Outlines"] = root.ref
	}
	return r.err
}

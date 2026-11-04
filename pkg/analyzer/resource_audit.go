package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/models"
)

// ResourceAudit 记录资源依赖闭包；unobserved 只表示在本次检查范围内未观测到可达引用，不是删除许可。
type ResourceAudit struct {
	Status     string               `json:"status"`
	Scope      string               `json:"scope"`
	Nodes      []ResourceNode       `json:"nodes"`
	Edges      []ResourceDependency `json:"edges"`
	Unobserved int                  `json:"unobserved"`
}

// ResourceNode 表示文档作用域内的一项资源定义或未解析的引用。
type ResourceNode struct {
	Key           string `json:"key"`
	DocumentIndex int    `json:"document_index"`
	Kind          string `json:"kind"`
	ID            uint64 `json:"id"`
	SourceFile    string `json:"source_file,omitempty"`
	Declared      bool   `json:"declared"`
	Reachable     bool   `json:"reachable"`
}

// ResourceDependency 描述定义间的依赖；Root 表示 From 是页面等入口文件，而不是资源。
type ResourceDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
	Root bool   `json:"root"`
}

type resourceGraph struct {
	nodes    map[string]ResourceNode
	edges    map[ResourceDependency]struct{}
	patterns map[*models.CtPattern]bool
}

func (a *analyzer) auditOutlines(document int, outlines *models.OutlineList, source string) {
	if outlines == nil {
		return
	}
	var visit func([]models.CTOutlineElem)
	visit = func(items []models.CTOutlineElem) {
		for _, item := range items {
			if item.Actions != nil {
				a.auditActions(document, item.Actions.Actions, "", source)
			}
			visit(item.OutlineElem)
		}
	}
	visit(outlines.OutlineElems)
}

func (a *analyzer) auditGraph() *resourceGraph {
	if a.resourceGraph == nil {
		a.resourceGraph = &resourceGraph{nodes: make(map[string]ResourceNode), edges: make(map[ResourceDependency]struct{}), patterns: make(map[*models.CtPattern]bool)}
	}
	return a.resourceGraph
}

func (a *analyzer) auditRef(owner, source string, document int, kind string, id uint64) string {
	if id == 0 {
		return ""
	}
	graph := a.auditGraph()
	key := displayResourceKey(document, kind, models.StID(id))
	if _, ok := graph.nodes[key]; !ok {
		graph.nodes[key] = ResourceNode{Key: key, DocumentIndex: document, Kind: kind, ID: id}
	}
	root := owner == ""
	if root {
		owner = source
	}
	graph.edges[ResourceDependency{From: owner, To: key, Root: root}] = struct{}{}
	return key
}

func (a *analyzer) auditContent(document int, content *models.PageContent, owner, source string) {
	if content == nil {
		return
	}
	if a.options.IncludeTemplates {
		for _, template := range content.Template {
			a.auditRef(owner, source, document, "template", uint64(template.TemplateID))
		}
	}
	if content.Actions != nil {
		a.auditActions(document, content.Actions.Action, owner, source)
	}
	if content.Content == nil {
		return
	}
	for _, layer := range content.Content.Layer {
		if layer == nil {
			continue
		}
		a.auditRef(owner, source, document, "draw-param", uint64(layer.DrawParam))
		a.auditItems(document, layer.Items, owner, source)
	}
}

// 只收集引用，不把模板、复合图元、Pattern 或裁剪文字混入页面对象和文字统计。
func (a *analyzer) auditItems(document int, items []models.PageItem, owner, source string) {
	for _, item := range items {
		var unit *models.CTGraphicUnit
		switch item.Kind {
		case models.PageItemText:
			unit = &item.Text.CTGraphicUnit
			a.auditRef(owner, source, document, "font", uint64(item.Text.Font))
			a.auditColor(document, item.Text.FillColor, owner, source)
			a.auditColor(document, item.Text.StrokeColor, owner, source)
		case models.PageItemPath:
			unit = &item.Path.CTGraphicUnit
			a.auditColor(document, item.Path.FillColor, owner, source)
			a.auditColor(document, item.Path.StrokeColor, owner, source)
		case models.PageItemImage:
			unit = &item.Image.CTGraphicUnit
			for _, id := range []models.StRefID{item.Image.ResourceID, item.Image.Substitution, item.Image.ImageMask} {
				a.auditRef(owner, source, document, "image", uint64(id))
			}
			if item.Image.Border != nil {
				a.auditColor(document, item.Image.Border.BorderColor, owner, source)
			}
		case models.PageItemComposite:
			unit = &item.Composite.CTGraphicUnit
			a.auditRef(owner, source, document, "composite", uint64(item.Composite.ResourceID))
		case models.PageItemBlock:
			a.auditItems(document, item.Block.Items, owner, source)
		}
		if unit == nil {
			continue
		}
		a.auditRef(owner, source, document, "draw-param", uint64(unit.DrawParam))
		if unit.Actions != nil {
			a.auditActions(document, unit.Actions.Action, owner, source)
		}
		if unit.Clips != nil {
			for _, clip := range unit.Clips.Clip {
				for _, area := range clip.Area {
					if area.DrawParam != nil {
						a.auditRef(owner, source, document, "draw-param", uint64(*area.DrawParam))
					}
					if area.Text != nil {
						a.auditRef(owner, source, document, "font", uint64(area.Text.Font))
					}
				}
			}
		}
	}
}

func (a *analyzer) auditActions(document int, actions []models.CtAction, owner, source string) {
	for _, action := range actions {
		if action.Sound != nil {
			a.auditRef(owner, source, document, "image", uint64(action.Sound.ResourceID))
		}
		if action.Movie != nil {
			a.auditRef(owner, source, document, "image", uint64(action.Movie.ResourceID))
		}
	}
}

func (a *analyzer) auditColor(document int, color *models.CTColor, owner, source string) {
	if color == nil {
		return
	}
	a.auditRef(owner, source, document, "color-space", uint64(color.ColorSpace))
	if color.Pattern != nil {
		a.registerPattern(document, color)
		internal := a.patternKeys[color.Pattern]
		parts := strings.Split(internal, ":")
		id, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
		key := a.auditRef(owner, source, document, "pattern", id)
		graph := a.auditGraph()
		if !graph.patterns[color.Pattern] {
			graph.patterns[color.Pattern] = true
			node := graph.nodes[key]
			node.Declared, node.SourceFile = true, source
			graph.nodes[key] = node
			a.auditRef(key, source, document, "image", uint64(color.Pattern.CellContent.Thumbnail))
			a.auditItems(document, color.Pattern.CellContent.Items, key, source)
		}
	}
	if color.AxialShd != nil {
		for i := range color.AxialShd.Segment {
			a.auditColor(document, &color.AxialShd.Segment[i].Color, owner, source)
		}
	}
	if color.RadialShd != nil {
		for i := range color.RadialShd.Segment {
			a.auditColor(document, &color.RadialShd.Segment[i].Color, owner, source)
		}
	}
	if color.GouraudShd != nil {
		a.auditColor(document, color.GouraudShd.BackColor, owner, source)
		for i := range color.GouraudShd.Point {
			a.auditColor(document, &color.GouraudShd.Point[i].Color, owner, source)
		}
	}
	for _, shading := range []*models.CTLaGouraudShd{color.LaGouraudShd, color.LaGourandShd} {
		if shading == nil {
			continue
		}
		a.auditColor(document, shading.BackColor, owner, source)
		for i := range shading.Point {
			a.auditColor(document, &shading.Point[i].Color, owner, source)
		}
	}
}

func (a *analyzer) finishResourceAudit() {
	graph := a.auditGraph()
	for _, resource := range a.resources {
		key := displayResourceKey(resource.DocumentIndex, resource.Kind, models.StID(resource.ID))
		node := graph.nodes[key]
		node.Key, node.DocumentIndex, node.Kind, node.ID = key, resource.DocumentIndex, resource.Kind, resource.ID
		node.Declared, node.SourceFile = true, resource.SourceFile
		graph.nodes[key] = node
	}
	adjacent := make(map[string][]string)
	var queue []string
	for edge := range graph.edges {
		if edge.Root {
			queue = append(queue, edge.To)
		} else {
			adjacent[edge.From] = append(adjacent[edge.From], edge.To)
		}
	}
	for len(queue) > 0 {
		key := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		node := graph.nodes[key]
		if node.Reachable {
			continue
		}
		node.Reachable = true
		graph.nodes[key] = node
		queue = append(queue, adjacent[key]...)
	}
	audit := ResourceAudit{Status: "complete", Scope: "页面及启用的注解为入口，递归模板、复合图元、Pattern、绘制参数继承、颜色空间、图像替代/遮罩、裁剪文字及音视频动作引用；未观测到引用不代表可安全删除", Nodes: make([]ResourceNode, 0, len(graph.nodes)), Edges: make([]ResourceDependency, 0, len(graph.edges))}
	for _, stage := range a.report.Stages {
		if stage.Name == "documents" || stage.Name == "pages" || stage.Name == "resources" || stage.Name == "templates" || stage.Name == "annotations" {
			if stage.Status != "complete" {
				audit.Status = "partial"
			}
		}
	}
	for _, node := range graph.nodes {
		if node.Declared && !node.Reachable {
			audit.Unobserved++
		}
		audit.Nodes = append(audit.Nodes, node)
	}
	for edge := range graph.edges {
		audit.Edges = append(audit.Edges, edge)
	}
	sort.Slice(audit.Nodes, func(i, j int) bool { return audit.Nodes[i].Key < audit.Nodes[j].Key })
	for _, node := range audit.Nodes {
		if node.Reachable && !node.Declared {
			document := node.DocumentIndex
			a.finding(Diagnostic{Code: "resource.unresolved_dependency", Severity: "warning", Stage: "resources", DocumentIndex: &document, ObjectID: strconv.FormatUint(node.ID, 10),
				Message: "可达资源引用未找到定义：" + node.Key, Basis: "GB/T 33190—2016 第 8.3、9、13 章；按文档体作用域计算引用闭包"}, false)
		}
	}
	sort.Slice(audit.Edges, func(i, j int) bool {
		l, r := audit.Edges[i], audit.Edges[j]
		if l.From != r.From {
			return l.From < r.From
		}
		if l.To != r.To {
			return l.To < r.To
		}
		return !l.Root && r.Root
	})
	a.report.ResourceAudit = audit
}

func (a *analyzer) unobservedResources(kind string, definitions map[string]int) int {
	count := 0
	for key, declarations := range definitions {
		var document int
		var id uint64
		_, _ = fmt.Sscanf(key, "%d:"+kind+":%d", &document, &id)
		if !a.auditGraph().nodes[displayResourceKey(document, kind, models.StID(id))].Reachable {
			count += max(1, declarations)
		}
	}
	return count
}

package render

import (
	"strings"

	"github.com/zc310/ofd/internal/models"
)

// Diagnostic 是一次绘制中的可恢复问题；同一原因、对象和资源会合并计数。
type Diagnostic struct {
	Code       string
	Message    string
	ObjectID   models.StID
	ResourceID models.StID
	Count      int
}

// Stats 统计绘制遍历和计入预算的工作，离屏重画重复计数。
// 它不是静态图元总数或内存峰值；OffscreenPixels 仅包含计入渲染预算的离屏像素。
type Stats struct {
	ObjectsVisited      int
	CompositeExpansions int
	PatternTiles        int
	OffscreenPixels     float64
}

// Report 是单次绘制的独立快照，不记录到共享文档或全局日志处理器。
type Report struct {
	PageID      models.StID
	Stats       Stats
	Diagnostics []Diagnostic
	// Dropped 是超过条目上限后未保存的诊断事件数。
	Dropped int
}

const maxRenderDiagnostics = 128

type diagnosticKey struct {
	code             string
	object, resource models.StID
}
type renderReportBuilder struct {
	Report

	indexes map[diagnosticKey]int
}

func (b *renderBudget) diagnose(code, message string, resource models.StID) {
	if b == nil || b.report == nil {
		return
	}
	r := b.report
	key := diagnosticKey{code, b.objectID, resource}
	if index, ok := r.indexes[key]; ok {
		r.Diagnostics[index].Count++
		return
	}
	if len(r.Diagnostics) >= maxRenderDiagnostics {
		r.Dropped++
		return
	}
	if r.indexes == nil {
		r.indexes = make(map[diagnosticKey]int)
	}
	r.indexes[key] = len(r.Diagnostics)
	if len(message) > 512 {
		message = strings.Clone(message[:512])
	}
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Message: message, ObjectID: b.objectID, ResourceID: resource, Count: 1})
}

func (r *renderReportBuilder) snapshot() Report {
	if r == nil {
		return Report{}
	}
	report := r.Report
	report.Diagnostics = append([]Diagnostic(nil), r.Diagnostics...)
	return report
}

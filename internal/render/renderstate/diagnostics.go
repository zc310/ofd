package renderstate

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

// MaxDiagnostics 限制一份报告保留的诊断条目数，超出后只累计 Dropped。
const MaxDiagnostics = 128

type diagnosticKey struct {
	code             string
	object, resource models.StID
}

// ReportBuilder 聚合单次绘制的诊断，零值可用。
type ReportBuilder struct {
	Report

	indexes map[diagnosticKey]int
}

// Diagnose 按原因、对象与资源合并计数，并限制诊断条目及消息长度。
func (b *Budget) Diagnose(code, message string, resource models.StID) {
	if b == nil || b.Report == nil {
		return
	}
	r := b.Report
	key := diagnosticKey{code, b.ObjectID, resource}
	if index, ok := r.indexes[key]; ok {
		r.Diagnostics[index].Count++
		return
	}
	if len(r.Diagnostics) >= MaxDiagnostics {
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
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Message: message, ObjectID: b.ObjectID, ResourceID: resource, Count: 1})
}

// Snapshot 返回独立的报告副本，调用方修改诊断切片不会污染聚合器。
func (r *ReportBuilder) Snapshot() Report {
	if r == nil {
		return Report{}
	}
	report := r.Report
	report.Diagnostics = append([]Diagnostic(nil), r.Diagnostics...)
	return report
}

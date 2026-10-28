package render

import "github.com/zc310/ofd/internal/render/renderstate"

// Diagnostic 是一次绘制中的可恢复问题；同一原因、对象和资源会合并计数。
type Diagnostic = renderstate.Diagnostic

// Stats 统计绘制遍历和计入预算的工作，离屏重画重复计数。
type Stats = renderstate.Stats

// Report 是单次绘制的独立快照。
type Report = renderstate.Report

type renderReportBuilder = renderstate.ReportBuilder

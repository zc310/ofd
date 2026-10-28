package renderstate

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestDiagnosticsBoundMessagesAggregateAndReturnIndependentSnapshots(t *testing.T) {
	report := &ReportBuilder{}
	budget := Budget{Report: report, ObjectID: 42}
	for range 3 {
		budget.Diagnose("missing", strings.Repeat("x", 600), 7)
	}
	for i := range MaxDiagnostics {
		budget.ObjectID = models.StID(100 + i)
		budget.Diagnose("missing", "item", 7)
	}
	got := report.Snapshot()
	if len(got.Diagnostics) != MaxDiagnostics || got.Dropped != 1 || got.Diagnostics[0].Count != 3 || len(got.Diagnostics[0].Message) != 512 {
		t.Fatalf("诊断聚合或上限错误：%+v", got)
	}
	got.Diagnostics[0].Code = "changed"
	if report.Snapshot().Diagnostics[0].Code != "missing" {
		t.Fatal("快照修改污染了聚合器")
	}
	var absent *Budget
	absent.Diagnose("ignored", "ignored", 0)
	var absentReport *ReportBuilder
	if got := absentReport.Snapshot(); len(got.Diagnostics) != 0 {
		t.Fatal("空报告应返回零值")
	}
}

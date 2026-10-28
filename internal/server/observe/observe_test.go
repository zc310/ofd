package observe

import (
	"strings"
	"testing"
	"time"
)

func TestWritePrometheusHistogramAndNotifications(t *testing.T) {
	r := NewRegistry()
	labels := Conversion{Lane: "fast", From: "docx", To: "pdf", State: "succeeded"}
	r.ObserveConversion(labels, 150*time.Millisecond)
	r.ObserveConversion(labels, 2*time.Second)
	r.ObserveNotification(Notification{Target: `a"b`, Event: "succeeded", Result: ResultSuccess})
	r.ObserveNotification(Notification{Target: `a"b`, Event: "succeeded", Result: ResultFailure})

	var b strings.Builder
	r.WritePrometheus(&b)
	out := b.String()
	for _, want := range []string{
		`ofd_conversion_duration_seconds_bucket{lane="fast",from="docx",to="pdf",state="succeeded",le="0.25"} 1`,
		`ofd_conversion_duration_seconds_bucket{lane="fast",from="docx",to="pdf",state="succeeded",le="2.5"} 2`,
		`ofd_conversion_duration_seconds_bucket{lane="fast",from="docx",to="pdf",state="succeeded",le="+Inf"} 2`,
		`ofd_conversion_duration_seconds_sum{lane="fast",from="docx",to="pdf",state="succeeded"} 2.15`,
		`ofd_conversion_duration_seconds_count{lane="fast",from="docx",to="pdf",state="succeeded"} 2`,
		`ofd_notifications_total{target="a\"b",event="succeeded",result="failure"} 1`,
		`ofd_notifications_total{target="a\"b",event="succeeded",result="success"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %s:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "# TYPE ofd_conversion_duration_seconds histogram") {
		t.Error("缺少 histogram TYPE")
	}
	if !strings.Contains(out, "# TYPE ofd_notifications_total counter") {
		t.Error("缺少 counter TYPE")
	}
}

func TestNilRegistryAndDefaultFailure(t *testing.T) {
	var nilRegistry *Registry
	nilRegistry.ObserveConversion(Conversion{}, time.Second)
	nilRegistry.ObserveNotification(Notification{})
	var b strings.Builder
	nilRegistry.WritePrometheus(&b)
	if b.Len() != 0 {
		t.Errorf("nil registry 不应输出内容: %q", b.String())
	}

	r := NewRegistry()
	r.ObserveNotification(Notification{Target: "t", Event: "failed"})
	b.Reset()
	r.WritePrometheus(&b)
	if !strings.Contains(b.String(), `result="failure"} 1`) {
		t.Errorf("空结果应归为 failure:\n%s", b.String())
	}
}

package converter

import (
	"bytes"
	"testing"
)

// 端到端 OFD→PDF 的速度基准，用四个形态差异较大的样例：
//
//	intro.ofd  42 页 7.1 MB  12 个内嵌字体，页数与字体都最多，考察全链路
//	zsbk.ofd    2 页 1.5 MB  11 个内嵌字体且解压后占包体积 111%，纯字体负载
//	999.ofd     5 页  29 KB  无字体无图，以矢量文字为主，考察逐页固定成本
//	ano.ofd     3 页 701 KB  含签章注解与大图，考察图像路径
//
// 之前只有 BenchmarkRenderPDFIntro 一个，改动渲染路径后缺少其它形态的对照，
// 容易只看字体密集样例的涨跌而漏掉别的退化。四个一起跑才看得出改动落在哪一段，
// 也便于区分"字体解析变慢"与"逐页开销变慢"——只看 intro 无法区分。
//
// 输出走 bytes.Buffer 而不是文件：PDF 写入是纯内存追加，落盘会把磁盘噪声混进
// 计时，也会让重复运行互相干扰。
func benchmarkRenderPDF(b *testing.B, name string) {
	b.Helper()
	var output bytes.Buffer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		output.Reset()
		if err := PDF(ctxTODO, "../../testdata/"+name+".ofd", &output); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	// 报告产物大小：页数或内容变化会体现在这里，比单看 ns/op 更容易发现
	// "变快了但少渲染了东西"这类问题。
	b.ReportMetric(float64(output.Len()), "output_bytes")
}

func BenchmarkRenderPDFIntro(b *testing.B) { benchmarkRenderPDF(b, "intro") }

func BenchmarkRenderPDFZsbk(b *testing.B) { benchmarkRenderPDF(b, "zsbk") }

func BenchmarkRenderPDF999(b *testing.B) { benchmarkRenderPDF(b, "999") }

func BenchmarkRenderPDFAno(b *testing.B) { benchmarkRenderPDF(b, "ano") }

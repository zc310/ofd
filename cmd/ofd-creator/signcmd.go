package main

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"

	"github.com/spf13/pflag"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/sign"
)

type signatureCollector struct {
	mu     sync.Mutex
	events []merge.SignatureEvent
}

func (c *signatureCollector) add(event merge.SignatureEvent) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

func (c *signatureCollector) snapshot() []merge.SignatureEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]merge.SignatureEvent(nil), c.events...)
}

func reportSignatureEvents(w io.Writer, events []merge.SignatureEvent) {
	if len(events) == 0 {
		return
	}
	var preserved, rewritten, dropped int
	for _, event := range events {
		switch event.Action {
		case merge.SignaturePreserved:
			preserved++
		case merge.SignatureRewritten:
			rewritten++
		case merge.SignatureDropped:
			dropped++
		}
		_, _ = fmt.Fprintf(w, "ofd-creator merge: 签名 %s#%d %s -> %s\n", event.Input, event.DocumentIndex, event.ID, event.Action)
	}
	_, _ = fmt.Fprintf(w, "ofd-creator merge: 签名汇总：保留 %d，重写 %d，丢弃 %d\n", preserved, rewritten, dropped)
}

func reportSignatureVerification(w io.Writer, prefix string, statuses []merge.SignatureStatus) {
	if len(statuses) == 0 {
		_, _ = fmt.Fprintln(w, prefix+": 输出文档没有签名")
		return
	}
	for _, status := range statuses {
		digest := "摘要有效"
		if !status.DigestValid {
			digest = "摘要无效"
		}
		signature := "密码学签名未验证"
		switch {
		case status.Verified:
			signature = "密码学签名有效"
		case status.VerificationError != "":
			signature = "密码学签名校验失败"
		}
		_, _ = fmt.Fprintf(w, "%s: 签名 %s：%s，%s\n", prefix, status.ID, digest, signature)
	}
}

func signStamp(opts *signFlags) *sign.StampOptions {
	if !opts.signStamp {
		return nil
	}
	return &sign.StampOptions{PageRef: opts.signStampPage, Boundary: opts.signStampBoundary}
}

func signStampSeam(opts *signFlags) *sign.StampSeamOptions {
	if !opts.signStampSeams {
		return nil
	}
	return &sign.StampSeamOptions{
		Edge:       opts.signStampSeamEdge,
		Pages:      opts.signStampSeamPages,
		Pieces:     opts.signStampSeamPieces,
		GroupPages: opts.signStampSeamGroupPages,
		Size:       opts.signStampSeamSize,
		MinStrip:   opts.signStampSeamMinStrip,
		X:          opts.signStampSeamX,
		Y:          opts.signStampSeamY,
	}
}

func signReferences(opts *signFlags) *sign.ReferenceOptions {
	if len(opts.signInclude) == 0 && len(opts.signExclude) == 0 && !opts.signRoot {
		return nil
	}
	return &sign.ReferenceOptions{Include: opts.signInclude, Exclude: opts.signExclude, RootDocument: opts.signRoot}
}

func buildSignOptions(opts *signFlags, deterministic bool) sign.Options {
	return sign.Options{
		Command:         opts.signCmd,
		ID:              opts.signID,
		ProviderName:    opts.signProvider,
		ProviderVersion: opts.signProviderVersion,
		Company:         opts.signCompany,
		SignatureMethod: opts.signMethod,
		CheckMethod:     opts.signCheckMethod,
		Deterministic:   deterministic,
		Stamp:           signStamp(opts),
		StampSeam:       signStampSeam(opts),
		Seal:            opts.signSeal,
		References:      signReferences(opts),
	}
}

func registerSignFlags(flags *pflag.FlagSet, opts *signFlags) {
	flags.StringVar(&opts.signCmd, "sign-cmd", "", "调用外部命令为输出追加签名；命令从 stdin 读 Signature.xml，向 stdout 写 SignedValue.dat")
	flags.StringVar(&opts.signID, "sign-id", "sign-1", "外部签名的签名标识，需为合法 xs:ID")
	flags.StringVar(&opts.signProvider, "sign-provider", "", "外部签名写入 SignedInfo 的提供者名称")
	flags.StringVar(&opts.signProviderVersion, "sign-provider-version", "", "外部签名写入 SignedInfo 的提供者版本")
	flags.StringVar(&opts.signCompany, "sign-company", "", "外部签名写入 SignedInfo 的提供者公司")
	flags.StringVar(&opts.signMethod, "sign-method", "", "外部签名算法 OID，默认 "+sign.DefaultSignatureMethod)
	flags.StringVar(&opts.signCheckMethod, "sign-check-method", "", "外部签名摘要算法，默认 "+sign.DefaultCheckMethod)
	flags.BoolVar(&opts.signStamp, "sign-stamp", false, "在 Signature.xml 写入 StampAnnot，让阅读器绘制印章图片")
	flags.StringVar(&opts.signStampPage, "sign-stamp-page", "", "签章页面 ID，默认文档体首页")
	flags.StringVar(&opts.signStampBoundary, "sign-stamp-boundary", "", "签章位置 \"x y width height\"（毫米），默认首页右下角")
	flags.BoolVar(&opts.signStampSeams, "sign-stamp-seams", false, "在同一文档体各页面边缘写入骑缝章（默认右缘，见 --sign-stamp-seam-edge）")
	flags.StringVar(&opts.signStampSeamEdge, "sign-stamp-seam-edge", "right", "骑缝章贴靠边缘：left、right、top、bottom 或 all")
	flags.StringVar(&opts.signStampSeamPages, "sign-stamp-seam-pages", "all", "参与骑缝分片的页面：all、odd、even，或 1,3,5-7、11-、-10 等页码")
	flags.IntVar(&opts.signStampSeamPieces, "sign-stamp-seam-pieces", 0, "骑缝章拆分份数；0 按参与页面数计算")
	flags.IntVar(&opts.signStampSeamGroupPages, "sign-stamp-seam-group-pages", 0, "每组骑缝章覆盖页数；0 表示所有参与页共用一枚章")
	flags.Float64Var(&opts.signStampSeamSize, "sign-stamp-seam-size", 40, "骑缝章边长（毫米），默认 40")
	flags.Float64Var(&opts.signStampSeamMinStrip, "sign-stamp-seam-min-strip", 2, "每页骑缝条带最小宽度（毫米），默认 2；建议 4 或 8")
	flags.Float64Var(&opts.signStampSeamX, "sign-stamp-seam-x", -1, "上下边缘骑缝章左边 X 坐标（毫米），负值表示水平居中")
	flags.Float64Var(&opts.signStampSeamY, "sign-stamp-seam-y", -1, "骑缝章底边 Y 坐标（毫米），负值表示垂直居中")
	flags.StringVar(&opts.signSeal, "sign-seal", "", "独立电子印章文件（DER 编码的 .esl），打入签名目录并在 Signature.xml 中引用")
	flags.StringArrayVar(&opts.signInclude, "sign-include", nil, "签名引用白名单 glob（相对文档体目录，可重复）")
	flags.StringArrayVar(&opts.signExclude, "sign-exclude", nil, "签名引用排除 glob（相对文档体目录，可重复）")
	flags.BoolVar(&opts.signRoot, "sign-root", false, "把 OFD.xml 纳入签名引用")
}

// signFlags 是一组外部签名选项，merge 与 replace 共用。
type signFlags struct {
	signCmd                 string
	signID                  string
	signProvider            string
	signProviderVersion     string
	signCompany             string
	signMethod              string
	signCheckMethod         string
	signStamp               bool
	signStampPage           string
	signStampBoundary       string
	signStampSeams          bool
	signStampSeamEdge       string
	signStampSeamPages      string
	signStampSeamPieces     int
	signStampSeamGroupPages int
	signStampSeamSize       float64
	signStampSeamMinStrip   float64
	signStampSeamX          float64
	signStampSeamY          float64
	signSeal                string
	signInclude             []string
	signExclude             []string
	signRoot                bool
}

func validateSignFlags(opts *signFlags) error {
	requested := opts.signStamp || opts.signStampSeams || strings.TrimSpace(opts.signSeal) != ""
	if requested && strings.TrimSpace(opts.signCmd) == "" {
		return fmt.Errorf("指定签章参数时必须同时提供 --sign-cmd")
	}
	if opts.signStamp && opts.signStampSeams {
		return fmt.Errorf("--sign-stamp 与 --sign-stamp-seams 不能同时使用")
	}
	if opts.signStampSeams {
		for _, value := range []struct {
			name string
			v    float64
		}{
			{"--sign-stamp-seam-size", opts.signStampSeamSize},
			{"--sign-stamp-seam-min-strip", opts.signStampSeamMinStrip},
			{"--sign-stamp-seam-x", opts.signStampSeamX},
			{"--sign-stamp-seam-y", opts.signStampSeamY},
		} {
			if math.IsNaN(value.v) || math.IsInf(value.v, 0) {
				return fmt.Errorf("%s 必须是有限数值，实际 %v", value.name, value.v)
			}
		}
		if opts.signStampSeamSize < 0 {
			return fmt.Errorf("--sign-stamp-seam-size 不能为负数，实际 %g", opts.signStampSeamSize)
		}
		if opts.signStampSeamMinStrip < 0 {
			return fmt.Errorf("--sign-stamp-seam-min-strip 不能为负数，实际 %g", opts.signStampSeamMinStrip)
		}
		if opts.signStampSeamGroupPages < 0 {
			return fmt.Errorf("--sign-stamp-seam-group-pages 不能为负数，实际 %d", opts.signStampSeamGroupPages)
		}
		if opts.signStampSeamPieces < 0 {
			return fmt.Errorf("--sign-stamp-seam-pieces 不能为负数，实际 %d", opts.signStampSeamPieces)
		}
	}
	return nil
}

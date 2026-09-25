package main

import (
	"fmt"
	"io"
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
	flags.StringArrayVar(&opts.signInclude, "sign-include", nil, "签名引用白名单 glob（相对文档体目录，可重复）")
	flags.StringArrayVar(&opts.signExclude, "sign-exclude", nil, "签名引用排除 glob（相对文档体目录，可重复）")
	flags.BoolVar(&opts.signRoot, "sign-root", false, "把 OFD.xml 纳入签名引用")
}

// signFlags 是一组外部签名选项，merge 与 replace 共用。
type signFlags struct {
	signCmd             string
	signID              string
	signProvider        string
	signProviderVersion string
	signCompany         string
	signMethod          string
	signCheckMethod     string
	signStamp           bool
	signStampPage       string
	signStampBoundary   string
	signInclude         []string
	signExclude         []string
	signRoot            bool
}

package compare

import "context"

// 同时限制 Myers 编辑距离搜索和字符扫描工作量。超过上限的大片替换
// 明确回退为完整变化段落，保留双方原文，避免分配平方规模的字符矩阵。
const (
	maxTextEditDistance = 512
	maxTextDiffWork     = 4_000_000
)

func textDifferences(ctx context.Context, before, after string) ([]TextSpan, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if before == after {
		return nil, false, nil
	}
	a, b := []rune(before), []rune(after)
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		if prefix%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		if suffix%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		suffix++
	}
	left, right := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	middle, exact, err := myersText(ctx, left, right)
	if err != nil {
		return nil, false, err
	}
	if !exact {
		middle = []TextSpan{{Kind: "delete", Text: string(left)}, {Kind: "insert", Text: string(right)}}
	}
	spans := make([]TextSpan, 0, len(middle)+2)
	appendSpan := func(kind, text string) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Kind == kind {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, TextSpan{Kind: kind, Text: text})
		}
	}
	appendSpan("equal", string(a[:prefix]))
	for _, span := range middle {
		appendSpan(span.Kind, span.Text)
	}
	appendSpan("equal", string(a[len(a)-suffix:]))
	return spans, !exact, nil
}

type runeEdit struct {
	kind  string
	value rune
}

func myersText(ctx context.Context, a, b []rune) ([]TextSpan, bool, error) {
	if len(a) == 0 {
		return []TextSpan{{Kind: "insert", Text: string(b)}}, true, nil
	}
	if len(b) == 0 {
		return []TextSpan{{Kind: "delete", Text: string(a)}}, true, nil
	}
	limit := min(len(a)+len(b), maxTextEditDistance)
	offset := limit + 1
	v := make([]int, 2*limit+3)
	trace := make([][]int, 0, limit+1)
	work := 0
	for d := 0; d <= limit; d++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			work++
			if work > maxTextDiffWork {
				return nil, false, nil
			}
			x := 0
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < len(a) && y < len(b) && a[x] == b[y] {
				x++
				y++
				work++
				if work > maxTextDiffWork {
					return nil, false, nil
				}
				if work%4096 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, false, err
					}
				}
			}
			v[offset+k] = x
			if x >= len(a) && y >= len(b) {
				return backtrackText(a, b, trace, d, offset), true, nil
			}
		}
	}
	return nil, false, nil
}

func backtrackText(a, b []rune, trace [][]int, distance, offset int) []TextSpan {
	x, y := len(a), len(b)
	edits := make([]runeEdit, 0, len(a)+len(b))
	for d := distance; d >= 0; d-- {
		v := trace[d]
		k := x - y
		previousK := k - 1
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			previousK = k + 1
		}
		previousX := v[offset+previousK]
		previousY := previousX - previousK
		for x > previousX && y > previousY {
			x--
			y--
			edits = append(edits, runeEdit{"equal", a[x]})
		}
		if d == 0 {
			break
		}
		if x == previousX {
			y--
			edits = append(edits, runeEdit{"insert", b[y]})
		} else {
			x--
			edits = append(edits, runeEdit{"delete", a[x]})
		}
		x, y = previousX, previousY
	}
	spans := []TextSpan{}
	var run []rune
	kind := ""
	flush := func() {
		if len(run) > 0 {
			spans = append(spans, TextSpan{Kind: kind, Text: string(run)})
			run = run[:0]
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		if edits[i].kind != kind {
			flush()
			kind = edits[i].kind
		}
		run = append(run, edits[i].value)
	}
	flush()
	return spans
}

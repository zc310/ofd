package invoice

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SortItems 支持的排序方式。
const (
	// SortInput 保持输入顺序，是默认值。
	SortInput = "input"
	// SortDate 按开票日期升序。
	SortDate = "date"
	// SortCode 按发票代码与号码升序。
	SortCode = "code"
	// SortName 按来源名升序。
	SortName = "name"
)

// Source 是一个待抽取的发票输入。Input 的类型与 Extract 相同（文件路径、
// 字节切片或 io.Reader）；Name 用于报告与错误定位，为空时从路径推断。
type Source struct {
	Name  string
	Input any
}

// Item 是单个输入的处理结果。抽取失败时 Invoice 为 nil、Error 说明原因；
// 抽取成功但与先出现的发票重复时 DuplicateOf 指向那张发票的来源名。
type Item struct {
	Name        string   `json:"name"`
	Invoice     *Invoice `json:"invoice,omitempty"`
	Error       string   `json:"error,omitempty"`
	DuplicateOf string   `json:"duplicate_of,omitempty"`
	Issues      []string `json:"issues,omitempty"`
	Red         bool     `json:"red,omitempty"`

	skipped bool // 去重时未参与合计：仍出现在 Items 里，但计入 Skipped
	// source 是该结果对应的 Source 在输入列表中的下标。去重和排序都会改变
	// Items 的顺序与长度，合并等后续处理靠它找回原始输入，不能按下标对齐。
	source int
}

// OK 判断该输入是否抽取成功。
func (i Item) OK() bool {
	return i.Error == ""
}

// Skipped 记录未参与合计的输入及其原因。
type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// BatchOptions 控制批量抽取行为。
type BatchOptions struct {
	// SkipBroken 为 true 时，单个输入失败只记入该条结果并继续；默认 false，
	// 首个失败即整批返回错误。默认严格是为了避免一批票里静默少算金额——
	// 汇总少一张票和合并 PDF 少一页，在财务场景里都是事故。
	SkipBroken bool
	// Dedup 为 true 时，发票代码与号码重复的输入只保留第一张，其余移入
	// Skipped 并说明与哪张重复。默认 false：重复项仍然参与合计并被标记，
	// 丢弃与否交给使用者判断。
	Dedup bool
}

// Batch 是一批发票的抽取结果与汇总。
type Batch struct {
	// Items 按输入顺序排列的处理结果。
	Items []Item `json:"items"`
	// Skipped 是因去重而未参与合计的输入。
	Skipped []Skipped `json:"skipped,omitempty"`
	// Summary 是参与合计的发票的汇总。
	Summary Summary `json:"summary"`
}

// ExtractBatch 依次抽取 sources 中的发票并汇总金额。
//
// 默认严格：任一输入抽取失败即返回错误，避免整批金额在无人察觉时少算。
// BatchOptions.SkipBroken 打开后失败项记入 Item.Error 继续处理，
// BatchOptions.Dedup 打开后重复发票移入 Skipped。
func ExtractBatch(sources []Source, options BatchOptions) (*Batch, error) {
	if len(sources) == 0 {
		return nil, errors.New("至少需要一个发票输入")
	}
	batch := &Batch{}
	invoices := make([]*Invoice, 0, len(sources))
	byID := make(map[string]string, len(sources))
	for index, source := range sources {
		name := sourceName(source, index)
		result, err := Extract(source.Input)
		if err != nil {
			if !options.SkipBroken {
				return nil, fmt.Errorf("抽取 %s 失败: %w", name, err)
			}
			batch.Items = append(batch.Items, Item{Name: name, Error: err.Error(), source: index})
			continue
		}
		item := Item{Name: name, Invoice: result, Issues: result.Check(), Red: result.IsRed(), source: index}
		if id := result.ID(); id != "" {
			if first, seen := byID[id]; seen {
				item.DuplicateOf = first
			} else {
				byID[id] = name
			}
		}
		if options.Dedup && item.DuplicateOf != "" {
			item.skipped = true
			batch.Skipped = append(batch.Skipped, Skipped{
				Name:   name,
				Reason: "发票代码号码与 " + item.DuplicateOf + " 重复",
			})
		} else {
			invoices = append(invoices, result)
		}
		batch.Items = append(batch.Items, item)
	}
	batch.Summary = Summarize(invoices)
	return batch, nil
}

// ActiveItems 返回参与合计的输入，保持输入顺序。被去重跳过的项不在其中。
// 合并 PDF 等后续处理必须用它，而不是直接遍历 Items，否则会多合并一张票。
func (b *Batch) ActiveItems() []Item {
	if b == nil {
		return nil
	}
	items := make([]Item, 0, len(b.Items))
	for _, item := range b.Items {
		if !item.skipped {
			items = append(items, item)
		}
	}
	return items
}

// SortItems 按 key 就地重排 items，让同一批发票的汇总、报表和合并 PDF 保持同一次序。
// 支持 SortInput（保持输入顺序）、SortDate（开票日期）、SortCode（发票代码与号码）
// 和 SortName（来源名）；无法排序的值排在最后，同值之间保持输入顺序。
func SortItems(items []Item, key string) error {
	var less func(a, b Item) bool
	switch key {
	case "", SortInput:
		return nil
	case SortDate:
		less = func(a, b Item) bool { return sortKeyTime(a).Before(sortKeyTime(b)) }
	case SortCode:
		less = func(a, b Item) bool {
			left, right := sortKeyCode(a), sortKeyCode(b)
			if left != right {
				return left < right
			}
			return a.Name < b.Name
		}
	case SortName:
		less = func(a, b Item) bool { return a.Name < b.Name }
	default:
		return fmt.Errorf("未知的排序方式 %q，可用 %s、%s、%s、%s",
			key, SortInput, SortDate, SortCode, SortName)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.Invoice == nil) != (b.Invoice == nil) {
			return b.Invoice == nil
		}
		return less(a, b)
	})
	return nil
}

// sortKeyTime 返回排序用的时间，无法解析或缺日期时返回零值时间（排在最后）。
func sortKeyTime(item Item) time.Time {
	if item.Invoice == nil {
		return time.Time{}
	}
	issued, err := item.Invoice.InvoiceDate()
	if err != nil {
		return time.Time{}
	}
	return issued
}

// sortKeyCode 返回发票代码与号码组成的排序键。
func sortKeyCode(item Item) string {
	if item.Invoice == nil {
		return ""
	}
	return item.Invoice.Code + "/" + item.Invoice.Number
}

// sourceName 返回输入的来源名，缺省时用序号占位，保证报告里每行都有名字。
func sourceName(source Source, index int) string {
	if name := strings.TrimSpace(source.Name); name != "" {
		return name
	}
	if path, ok := source.Input.(string); ok {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			return trimmed
		}
	}
	return fmt.Sprintf("输入 %d", index+1)
}

// Summarize 汇总一组发票的金额，并按销售方、发票类型和开票月份分组。
//
// 红字（红冲）发票的负金额照常参与求和。合计按「分」四舍五入后输出：不做舍入的
// 求和会展开成 64 位小数那样的长数字，财务无法对账。某项金额在所有发票里都缺失
// 时该字段为 nil，而不是 0。
func Summarize(invoices []*Invoice) Summary {
	present := make([]*Invoice, 0, len(invoices))
	for _, item := range invoices {
		if item != nil {
			present = append(present, item)
		}
	}
	amounts := make([]*Decimal, 0, len(present))
	taxes := make([]*Decimal, 0, len(present))
	totals := make([]*Decimal, 0, len(present))
	red := 0
	for _, item := range present {
		amounts = append(amounts, item.Amount)
		taxes = append(taxes, item.TaxAmount)
		totals = append(totals, item.TotalAmount)
		if item.IsRed() {
			red++
		}
	}
	summary := Summary{
		Count:       len(present),
		Red:         red,
		Amount:      Sum(amounts).Money(),
		TaxAmount:   Sum(taxes).Money(),
		TotalAmount: Sum(totals).Money(),
	}
	summary.BySeller = groupInvoices(present, sellerGroup)
	summary.ByType = groupInvoices(present, typeGroup)
	summary.ByMonth = groupInvoices(present, monthGroup)
	return summary
}

// Group 是一个维度的汇总行。Key 是分组键（税号、发票类型或 YYYY-MM），
// Name 是展示名；某张票在该维度上没有值时归入「未知」而不是被丢弃。
type Group struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Count       int      `json:"count"`
	Red         int      `json:"red,omitempty"`
	Amount      *Decimal `json:"amount,omitempty"`
	TaxAmount   *Decimal `json:"tax_amount,omitempty"`
	TotalAmount *Decimal `json:"total_amount,omitempty"`
}

// Summary 是一组发票的合计与分组。
type Summary struct {
	// Count 是参与汇总的张数。
	Count int `json:"count"`
	// Red 是其中红字（红冲）发票的张数。
	Red int `json:"red,omitempty"`
	// Amount 是不含税金额合计，TaxAmount 是税额合计，TotalAmount 是价税合计。
	Amount      *Decimal `json:"amount,omitempty"`
	TaxAmount   *Decimal `json:"tax_amount,omitempty"`
	TotalAmount *Decimal `json:"total_amount,omitempty"`
	// BySeller、ByType、ByMonth 是三个维度的分组合计，按价税合计降序排列。
	BySeller []Group `json:"by_seller,omitempty"`
	ByType   []Group `json:"by_type,omitempty"`
	ByMonth  []Group `json:"by_month,omitempty"`
}

// groupKeyFn 返回某张发票在某维度上的分组键与展示名。
type groupKeyFn func(*Invoice) (key string, name string)

func sellerGroup(inv *Invoice) (string, string) {
	code := strings.TrimSpace(inv.Seller.Code)
	name := strings.TrimSpace(inv.Seller.Name)
	if code != "" {
		return code, name
	}
	if name != "" {
		return name, name
	}
	return "未知", "未知销售方"
}

func typeGroup(inv *Invoice) (string, string) {
	if name := strings.TrimSpace(inv.Type); name != "" {
		return name, name
	}
	return "未知", "未知类型"
}

func monthGroup(inv *Invoice) (string, string) {
	issued, err := inv.InvoiceDate()
	if err != nil || issued.IsZero() {
		return "未知", "未知日期"
	}
	key := issued.Format("2006-01")
	return key, key
}

// addMoney 累加两个金额并按分舍入，任一为 nil 时只累加另一方，两方都缺失返回 nil。
func addMoney(total, value *Decimal) *Decimal {
	return Sum([]*Decimal{total, value}).Money()
}

// groupInvoices 按维度分组并求和，同维度内按价税合计降序、键名升序排列，
// 保证同一批发票重复汇总得到同一张表。
func groupInvoices(invoices []*Invoice, keyOf groupKeyFn) []Group {
	order := make([]string, 0, len(invoices))
	buckets := make(map[string]*Group, len(invoices))
	for _, inv := range invoices {
		key, name := keyOf(inv)
		group, seen := buckets[key]
		if !seen {
			group = &Group{Key: key, Name: name}
			buckets[key] = group
			order = append(order, key)
		}
		group.Count++
		if inv.IsRed() {
			group.Red++
		}
		// 用 Sum 而不是 Add：分组的首个金额从 nil 开始，Add 见到 nil 会返回 nil，
		// 整个分组的合计就会一直是 nil。Sum 逐项跳过 nil，全部缺失时才返回 nil。
		group.Amount = addMoney(group.Amount, inv.Amount)
		group.TaxAmount = addMoney(group.TaxAmount, inv.TaxAmount)
		group.TotalAmount = addMoney(group.TotalAmount, inv.TotalAmount)
	}
	groups := make([]Group, 0, len(order))
	for _, key := range order {
		groups = append(groups, *buckets[key])
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if cmp := groups[i].TotalAmount.Cmp(groups[j].TotalAmount); cmp != 0 {
			return cmp > 0
		}
		return groups[i].Key < groups[j].Key
	})
	return groups
}

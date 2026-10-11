package pdf2ofd

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Type 4 只允许 PDF 规范列出的计算操作符。过程仅由 if/ifelse 执行，
// 不提供名字定义、循环或文件访问；整数使用 PostScript 的有符号 32 位语义。
const calculatorOperators = " abs add atan ceiling cos cvi cvr div exp floor idiv ln log mod mul neg round sin sqrt sub truncate and bitshift eq ge gt le lt ne not or xor if ifelse copy dup exch index pop roll "
const calculatorStackLimit = 1024
const calculatorLocalWorkLimit = 100000

type calculatorValue struct {
	kind      byte // i 整数，r 实数，b 布尔，p 过程，o 操作符。
	number    float64
	boolean   bool
	procedure *calculatorProcedure
	operator  string
}

type calculatorProcedure struct{ values []calculatorValue }

type pdfCalculator struct {
	program  *calculatorProcedure
	inputs   int
	rng      []float64
	control  *conversionControl
	onError  func(error)
	reported bool
}

type calculatorCompiler struct {
	data      []byte
	pos, work int
	control   *conversionControl
}

func compilePDFCalculator(data []byte, domain, rng []float64, control *conversionControl) (*pdfCalculator, error) {
	for _, bounds := range [][]float64{domain, rng} {
		if len(bounds) == 0 || len(bounds)%2 != 0 || len(bounds)/2 > calculatorStackLimit {
			return nil, fmt.Errorf("Type 4 Domain/Range 的分量数无效")
		}
		for i := 0; i < len(bounds); i += 2 {
			if !calculatorFinite(bounds[i]) || !calculatorFinite(bounds[i+1]) || bounds[i] > bounds[i+1] {
				return nil, fmt.Errorf("Type 4 Domain/Range 区间无效")
			}
		}
	}
	compiler := calculatorCompiler{data: data, control: control}
	if err := compiler.skipSpace(); err != nil {
		return nil, err
	}
	if compiler.pos >= len(data) || data[compiler.pos] != '{' {
		return nil, fmt.Errorf("Type 4 函数必须以过程花括号包围")
	}
	program, err := compiler.procedure(1)
	if err != nil {
		return nil, err
	}
	if err := compiler.skipSpace(); err != nil {
		return nil, err
	}
	if compiler.pos != len(data) {
		return nil, fmt.Errorf("Type 4 过程后有多余内容")
	}
	return &pdfCalculator{program: program, inputs: len(domain) / 2, rng: rng, control: control}, nil
}

func calculatorFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func (c *calculatorCompiler) skipSpace() error {
	for c.pos < len(c.data) {
		if c.pos%4096 == 0 {
			if err := c.control.check(); err != nil {
				return err
			}
		}
		if isPDFWhitespace(c.data[c.pos]) {
			c.pos++
			continue
		}
		if c.data[c.pos] == '%' {
			for c.pos < len(c.data) && c.data[c.pos] != '\r' && c.data[c.pos] != '\n' {
				if c.pos%4096 == 0 {
					if err := c.control.check(); err != nil {
						return err
					}
				}
				c.pos++
			}
			continue
		}
		break
	}
	return c.control.check()
}

func (c *calculatorCompiler) consume() error {
	if err := c.control.consumeOperations(1); err != nil {
		return err
	}
	c.work++
	if c.work > calculatorLocalWorkLimit {
		return fmt.Errorf("Type 4 程序超过当前处理能力")
	}
	return nil
}

func calculatorDepth(control *conversionControl, depth int) error {
	maximum := int64(64)
	if control != nil {
		maximum = control.options.Limits.MaxDepth
	}
	if int64(depth) > maximum {
		if control != nil {
			return control.limit("function_depth", int64(depth), maximum)
		}
		return fmt.Errorf("Type 4 过程嵌套过深")
	}
	// 即使调用方增大预算，也避免在 Go 栈上递归数千层。
	if depth > 64 {
		return fmt.Errorf("Type 4 过程嵌套超过当前处理能力")
	}
	return nil
}

func (c *calculatorCompiler) procedure(depth int) (*calculatorProcedure, error) {
	if err := calculatorDepth(c.control, depth); err != nil {
		return nil, err
	}
	if err := c.consume(); err != nil {
		return nil, err
	}
	c.pos++ // {
	result := &calculatorProcedure{}
	for {
		if err := c.skipSpace(); err != nil {
			return nil, err
		}
		if c.pos == len(c.data) {
			return nil, fmt.Errorf("Type 4 过程未闭合")
		}
		if c.data[c.pos] == '}' {
			c.pos++
			return result, c.consume()
		}
		if c.data[c.pos] == '{' {
			proc, err := c.procedure(depth + 1)
			if err != nil {
				return nil, err
			}
			result.values = append(result.values, calculatorValue{kind: 'p', procedure: proc})
			continue
		}
		if err := c.consume(); err != nil {
			return nil, err
		}
		start := c.pos
		for c.pos < len(c.data) && !isPDFWhitespace(c.data[c.pos]) && !strings.ContainsRune("{}%", rune(c.data[c.pos])) {
			if c.pos%4096 == 0 {
				if err := c.control.check(); err != nil {
					return nil, err
				}
			}
			c.pos++
		}
		word := string(c.data[start:c.pos])
		var value calculatorValue
		switch {
		case word == "true" || word == "false":
			value = calculatorValue{kind: 'b', boolean: word == "true"}
		case strings.Contains(calculatorOperators, " "+word+" ") && word != "":
			value = calculatorValue{kind: 'o', operator: word}
		default:
			// PDF 的计算函数使用十进制数值，不接受 Go 的十六进制浮点语法。
			if word == "" || strings.IndexFunc(word, func(r rune) bool { return !(r >= '0' && r <= '9') && !strings.ContainsRune("+-.eE", r) }) >= 0 {
				return nil, fmt.Errorf("Type 4 不支持的操作符或数值 %q", word)
			}
			number, err := strconv.ParseFloat(word, 64)
			if err != nil || !calculatorFinite(number) {
				return nil, fmt.Errorf("Type 4 不支持的操作符或数值 %q", word)
			}
			value = calculatorValue{kind: 'r', number: number}
			if integer, err := strconv.ParseInt(word, 10, 32); err == nil {
				value.kind = 'i'
				value.number = float64(integer)
			}
		}
		result.values = append(result.values, value)
	}
}

type calculatorMachine struct {
	stack   []calculatorValue
	work    int
	control *conversionControl
}

func (f *pdfCalculator) eval(inputs []float64) ([]float64, bool) {
	machine := calculatorMachine{control: f.control}
	err := f.control.check()
	if len(inputs) != f.inputs {
		err = fmt.Errorf("Type 4 输入分量数量不匹配")
	}
	for _, input := range inputs {
		if !calculatorFinite(input) {
			err = fmt.Errorf("Type 4 输入不是有限数值")
			break
		}
		if err != nil {
			break
		}
		err = machine.push(calculatorValue{kind: 'r', number: input})
	}
	if err == nil {
		err = machine.execute(f.program, 1)
	}
	outputs := len(f.rng) / 2
	if err == nil && len(machine.stack) < outputs {
		err = fmt.Errorf("Type 4 输出栈不足")
	}
	var values []float64
	if err == nil {
		values = make([]float64, outputs)
		for i, value := range machine.stack[len(machine.stack)-outputs:] {
			if !value.numeric() || !calculatorFinite(value.number) {
				err = fmt.Errorf("Type 4 输出不是有限数值")
				break
			}
			values[i] = math.Max(f.rng[2*i], math.Min(f.rng[2*i+1], value.number))
		}
	}
	if err != nil {
		if !f.reported && f.onError != nil {
			f.reported = true
			f.onError(err)
		}
		return nil, false
	}
	return values, true
}

func (v calculatorValue) numeric() bool { return v.kind == 'i' || v.kind == 'r' }

func calculatorNumber(value float64, integer bool) (calculatorValue, error) {
	if !calculatorFinite(value) {
		return calculatorValue{}, fmt.Errorf("Type 4 算术结果不是有限数值")
	}
	kind := byte('r')
	if integer && value >= math.MinInt32 && value <= math.MaxInt32 && value == math.Trunc(value) {
		kind = 'i'
	}
	return calculatorValue{kind: kind, number: value}, nil
}

func (m *calculatorMachine) consume(count int) error {
	if count < 0 {
		return fmt.Errorf("Type 4 处理量无效")
	}
	if err := m.control.consumeOperations(int64(count)); err != nil {
		return err
	}
	if count < 0 || count > calculatorLocalWorkLimit-m.work {
		return fmt.Errorf("Type 4 求值超过当前处理能力")
	}
	m.work += count
	return nil
}

func (m *calculatorMachine) push(value calculatorValue) error {
	if len(m.stack) >= calculatorStackLimit {
		return fmt.Errorf("Type 4 操作数栈超过当前处理能力")
	}
	m.stack = append(m.stack, value)
	return nil
}

func (m *calculatorMachine) pop() (calculatorValue, error) {
	if len(m.stack) == 0 {
		return calculatorValue{}, fmt.Errorf("Type 4 操作数栈不足")
	}
	value := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return value, nil
}

func (m *calculatorMachine) integer() (int32, error) {
	v, err := m.pop()
	if err != nil {
		return 0, err
	}
	if v.kind != 'i' {
		return 0, fmt.Errorf("Type 4 操作数需要整数")
	}
	return int32(v.number), nil
}

func (m *calculatorMachine) execute(proc *calculatorProcedure, depth int) error {
	if err := calculatorDepth(m.control, depth); err != nil {
		return err
	}
	for _, value := range proc.values {
		if err := m.consume(1); err != nil {
			return err
		}
		if value.kind != 'o' {
			if err := m.push(value); err != nil {
				return err
			}
			continue
		}
		if err := m.operator(value.operator, depth); err != nil {
			return fmt.Errorf("Type 4 %s: %w", value.operator, err)
		}
	}
	return nil
}

func (m *calculatorMachine) operator(op string, depth int) error {
	switch op {
	case "pop":
		_, err := m.pop()
		return err
	case "dup":
		if len(m.stack) == 0 {
			return fmt.Errorf("操作数栈不足")
		}
		return m.push(m.stack[len(m.stack)-1])
	case "exch":
		if len(m.stack) < 2 {
			return fmt.Errorf("操作数栈不足")
		}
		i := len(m.stack) - 1
		m.stack[i], m.stack[i-1] = m.stack[i-1], m.stack[i]
		return nil
	case "copy", "index", "roll":
		return m.stackOperator(op)
	case "if", "ifelse":
		last, err := m.pop()
		if err != nil {
			return err
		}
		if last.kind != 'p' {
			return fmt.Errorf("需要过程操作数")
		}
		first := last
		if op == "ifelse" {
			first, err = m.pop()
			if err != nil {
				return err
			}
			if first.kind != 'p' {
				return fmt.Errorf("需要过程操作数")
			}
		}
		condition, err := m.pop()
		if err != nil {
			return err
		}
		if condition.kind != 'b' {
			return fmt.Errorf("条件需要布尔值")
		}
		if condition.boolean {
			return m.execute(first.procedure, depth+1)
		}
		if op == "ifelse" {
			return m.execute(last.procedure, depth+1)
		}
		return nil
	case "abs", "ceiling", "cos", "cvi", "cvr", "floor", "ln", "log", "neg", "round", "sin", "sqrt", "truncate", "not":
		return m.unary(op)
	default:
		return m.binary(op)
	}
}

func (m *calculatorMachine) stackOperator(op string) error {
	n, err := m.integer()
	if err != nil {
		return err
	}
	if op == "roll" {
		shift := n
		n, err = m.integer()
		if err != nil {
			return err
		}
		if n < 0 || int(n) > len(m.stack) {
			return fmt.Errorf("roll 的数量超出栈范围")
		}
		if n == 0 {
			return nil
		}
		if err := m.consume(int(n)); err != nil {
			return err
		}
		i := len(m.stack) - int(n)
		j := int(shift % n)
		if j < 0 {
			j += int(n)
		}
		copyOf := append([]calculatorValue(nil), m.stack[i:]...)
		for k, value := range copyOf {
			m.stack[i+(k+j)%int(n)] = value
		}
		return nil
	}
	if n < 0 || int(n) > len(m.stack) || (op == "index" && int(n) == len(m.stack)) {
		return fmt.Errorf("数量或索引超出栈范围")
	}
	if op == "index" {
		return m.push(m.stack[len(m.stack)-1-int(n)])
	}
	if int(n) > calculatorStackLimit-len(m.stack) {
		return fmt.Errorf("copy 超出操作数栈容量")
	}
	if err := m.consume(int(n)); err != nil {
		return err
	}
	m.stack = append(m.stack, m.stack[len(m.stack)-int(n):]...)
	return nil
}

func (m *calculatorMachine) unary(op string) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	if op == "not" {
		if v.kind == 'b' {
			v.boolean = !v.boolean
			return m.push(v)
		}
		if v.kind == 'i' {
			v.number = float64(^int32(v.number))
			return m.push(v)
		}
		return fmt.Errorf("not 需要布尔值或整数")
	}
	if !v.numeric() {
		return fmt.Errorf("需要数值操作数")
	}
	x := v.number
	integer := v.kind == 'i'
	switch op {
	case "abs":
		x = math.Abs(x)
	case "neg":
		x = -x
	case "ceiling":
		x = math.Ceil(x)
	case "floor":
		x = math.Floor(x)
	case "round":
		x = math.Floor(x + 0.5) // PostScript 中 -1.5 舍入到 -1。
	case "truncate":
		x = math.Trunc(x)
	case "cvi":
		x = math.Trunc(x)
		if x < math.MinInt32 || x > math.MaxInt32 {
			return fmt.Errorf("cvi 超出 32 位整数范围")
		}
		integer = true
	case "cvr":
		integer = false
	case "sqrt":
		x = math.Sqrt(x)
		integer = false
	case "ln":
		x = math.Log(x)
		integer = false
	case "log":
		x = math.Log10(x)
		integer = false
	case "sin":
		x = math.Sin(math.Mod(x, 360) * math.Pi / 180)
		integer = false
	case "cos":
		x = math.Cos(math.Mod(x, 360) * math.Pi / 180)
		integer = false
	}
	result, err := calculatorNumber(x, integer)
	if err != nil {
		return err
	}
	return m.push(result)
}

func (m *calculatorMachine) binary(op string) error {
	b, err := m.pop()
	if err != nil {
		return err
	}
	a, err := m.pop()
	if err != nil {
		return err
	}
	if op == "eq" || op == "ne" {
		equal := false
		if a.numeric() && b.numeric() {
			equal = a.number == b.number
		} else if a.kind == b.kind {
			if a.kind == 'b' {
				equal = a.boolean == b.boolean
			} else if a.kind == 'p' {
				equal = a.procedure == b.procedure
			}
		}
		if op == "ne" {
			equal = !equal
		}
		return m.push(calculatorValue{kind: 'b', boolean: equal})
	}
	if op == "and" || op == "or" || op == "xor" {
		if a.kind == 'b' && b.kind == 'b' {
			result := a.boolean && b.boolean
			if op == "or" {
				result = a.boolean || b.boolean
			} else if op == "xor" {
				result = a.boolean != b.boolean
			}
			return m.push(calculatorValue{kind: 'b', boolean: result})
		}
		if a.kind != 'i' || b.kind != 'i' {
			return fmt.Errorf("需要两个布尔值或两个整数")
		}
		x, y := int32(a.number), int32(b.number)
		value := x & y
		if op == "or" {
			value = x | y
		} else if op == "xor" {
			value = x ^ y
		}
		return m.push(calculatorValue{kind: 'i', number: float64(value)})
	}
	if !a.numeric() || !b.numeric() {
		return fmt.Errorf("需要数值操作数")
	}
	x, y := a.number, b.number
	if op == "ge" || op == "gt" || op == "le" || op == "lt" {
		result := x >= y
		switch op {
		case "gt":
			result = x > y
		case "le":
			result = x <= y
		case "lt":
			result = x < y
		}
		return m.push(calculatorValue{kind: 'b', boolean: result})
	}
	integer := a.kind == 'i' && b.kind == 'i'
	var result float64
	switch op {
	case "add":
		result = x + y
	case "sub":
		result = x - y
	case "mul":
		result = x * y
	case "div":
		result = x / y
		integer = false
	case "exp":
		result = math.Pow(x, y)
		integer = false
	case "atan":
		if x == 0 && y == 0 {
			return fmt.Errorf("atan 的两个操作数不能同时为零")
		}
		result = math.Atan2(x, y) * 180 / math.Pi
		if result < 0 {
			result += 360
		}
		integer = false
	case "idiv", "mod":
		if y == 0 || (op == "mod" && !integer) {
			return fmt.Errorf("除数必须非零；mod 需要整数操作数")
		}
		if op == "idiv" {
			result = math.Trunc(x / y)
			if result > math.MaxInt32 || result < math.MinInt32 || !calculatorFinite(result) {
				return fmt.Errorf("idiv 结果溢出")
			}
			integer = true
		} else {
			result = math.Mod(x, y)
		}
	case "bitshift":
		if !integer {
			return fmt.Errorf("bitshift 需要整数")
		}
		bits := uint32(int32(x))
		shift := int32(y)
		if shift >= 32 || shift <= -32 {
			bits = 0
		} else if shift >= 0 {
			bits <<= uint(shift)
		} else {
			bits >>= uint(-shift)
		}
		result = float64(int32(bits))
	default:
		return fmt.Errorf("未知的计算操作符 %q", op)
	}
	v, err := calculatorNumber(result, integer)
	if err != nil {
		return err
	}
	return m.push(v)
}

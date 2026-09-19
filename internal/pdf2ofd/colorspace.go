package pdf2ofd

import (
	"encoding/binary"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// pdfColorSpace 表示一个已解析的 PDF 颜色空间。颜色空间只描述分量语义，
// 具体颜色由 sc/scn/SC/SCN 操作数经 colorFromComponents 转换得到。
type pdfColorSpace struct {
	family     string
	components int
	base       *pdfColorSpace
	// Indexed：查找表与索引上界。
	lookup []byte
	high   int
	// Separation / DeviceN：tint 变换函数，把分量映射到 base 的分量。
	tint *pdfFunction
}

var (
	deviceGraySpace = &pdfColorSpace{family: "DeviceGray", components: 1}
	deviceRGBSpace  = &pdfColorSpace{family: "DeviceRGB", components: 3}
	deviceCMYKSpace = &pdfColorSpace{family: "DeviceCMYK", components: 4}
)

func namedColorSpace(name string) *pdfColorSpace {
	switch name {
	case "DeviceGray", "G":
		return deviceGraySpace
	case "DeviceRGB", "RGB":
		return deviceRGBSpace
	case "DeviceCMYK", "CMYK":
		return deviceCMYKSpace
	case "CalGray":
		return &pdfColorSpace{family: "CalGray", components: 1}
	case "CalRGB":
		return &pdfColorSpace{family: "CalRGB", components: 3}
	case "Lab":
		return &pdfColorSpace{family: "Lab", components: 3}
	default:
		return nil
	}
}

// resolveColorSpace 从页面资源字典解析颜色空间名称，失败时返回 nil。
func (p *pdfInterpreter) resolveColorSpace(resources types.Dict, name string) *pdfColorSpace {
	if name == "" {
		return nil
	}
	if resources != nil {
		if spaces, ok := dereferencedSubDict(p.ctx, resources, "ColorSpace"); ok {
			if object, found := spaces.Find(name); found {
				return p.parseColorSpace(object, 0)
			}
		}
	}
	return nil
}

// parseColorSpace 解析颜色空间对象，支持名称、数组和间接引用。
func (p *pdfInterpreter) parseColorSpace(object types.Object, depth int) *pdfColorSpace {
	if depth > 8 {
		return nil
	}
	object, err := dereferencePDFObject(p.ctx, object)
	if err != nil || object == nil {
		return nil
	}
	switch value := object.(type) {
	case types.Name:
		return namedColorSpace(value.Value())
	case types.Array:
		return p.parseColorSpaceArray(value, depth)
	default:
		return nil
	}
}

func (p *pdfInterpreter) parseColorSpaceArray(array types.Array, depth int) *pdfColorSpace {
	if len(array) == 0 {
		return nil
	}
	family, ok := dereferenceName(p.ctx, array[0])
	if !ok {
		return nil
	}
	switch family {
	case "ICCBased":
		return p.parseICCBasedSpace(array)
	case "Indexed", "I":
		return p.parseIndexedSpace(array, depth)
	case "Separation":
		return p.parseSeparationSpace(array, depth)
	case "DeviceN":
		return p.parseDeviceNSpace(array, depth)
	default:
		return namedColorSpace(family)
	}
}

func (p *pdfInterpreter) parseICCBasedSpace(array types.Array) *pdfColorSpace {
	if len(array) < 2 {
		return nil
	}
	profile, err := dereferencePDFObject(p.ctx, array[1])
	if err != nil {
		return nil
	}
	components := 3
	switch value := profile.(type) {
	case types.StreamDict:
		if n, ok := dereferencedPDFNumber(p.ctx, value.Dict["N"]); ok {
			components = int(n)
		}
	case *types.StreamDict:
		if n, ok := dereferencedPDFNumber(p.ctx, value.Dict["N"]); ok {
			components = int(n)
		}
	}
	switch components {
	case 1:
		return &pdfColorSpace{family: "ICCBased", components: 1, base: deviceGraySpace}
	case 4:
		return &pdfColorSpace{family: "ICCBased", components: 4, base: deviceCMYKSpace}
	default:
		return &pdfColorSpace{family: "ICCBased", components: 3, base: deviceRGBSpace}
	}
}

func (p *pdfInterpreter) parseIndexedSpace(array types.Array, depth int) *pdfColorSpace {
	if len(array) < 4 {
		return nil
	}
	base := p.parseColorSpace(array[1], depth+1)
	if base == nil {
		return nil
	}
	high, ok := dereferencedPDFNumber(p.ctx, array[2])
	if !ok || high < 0 || high > 255 {
		return nil
	}
	lookup, err := dereferencePDFObject(p.ctx, array[3])
	if err != nil {
		return nil
	}
	if stream, ok := lookup.(types.StreamDict); ok {
		if err := stream.Decode(); err != nil {
			return nil
		}
		lookup = stream
	}
	if stream, ok := lookup.(*types.StreamDict); ok {
		if err := stream.Decode(); err != nil {
			return nil
		}
		lookup = *stream
	}
	data, ok := pdfBytesObject(lookup)
	if !ok || len(data) < (int(high)+1)*base.components {
		return nil
	}
	return &pdfColorSpace{family: "Indexed", components: 1, base: base, lookup: data, high: int(high)}
}

func (p *pdfInterpreter) parseSeparationSpace(array types.Array, depth int) *pdfColorSpace {
	if len(array) < 4 {
		return nil
	}
	base := p.parseColorSpace(array[2], depth+1)
	if base == nil {
		return nil
	}
	tint := p.parseFunction(array[3], depth+1)
	if tint == nil {
		return nil
	}
	return &pdfColorSpace{family: "Separation", components: 1, base: base, tint: tint}
}

func (p *pdfInterpreter) parseDeviceNSpace(array types.Array, depth int) *pdfColorSpace {
	if len(array) < 4 {
		return nil
	}
	names, err := dereferencePDFObject(p.ctx, array[1])
	if err != nil {
		return nil
	}
	components := 1
	if list, ok := names.(types.Array); ok {
		components = len(list)
	}
	base := p.parseColorSpace(array[2], depth+1)
	if base == nil {
		return nil
	}
	tint := p.parseFunction(array[3], depth+1)
	if tint == nil {
		return nil
	}
	return &pdfColorSpace{family: "DeviceN", components: components, base: base, tint: tint}
}

// colorFromComponents 把颜色空间分量转换为设备 RGB。分量数量与颜色空间
// 不匹配时返回 false，调用方应保留上一个颜色。
func (cs *pdfColorSpace) colorFromComponents(values []float64) (pdfColor, bool) {
	if cs == nil {
		return pdfColor{}, false
	}
	switch cs.family {
	case "Indexed":
		if len(values) < 1 {
			return pdfColor{}, false
		}
		// Indexed 的操作数是整数索引，不是归一化分量。
		index := int(math.Round(values[0]))
		if index < 0 || index > cs.high || cs.base == nil {
			return pdfColor{}, false
		}
		offset := index * cs.base.components
		components := make([]float64, cs.base.components)
		for i := range components {
			if offset+i >= len(cs.lookup) {
				return pdfColor{}, false
			}
			components[i] = float64(cs.lookup[offset+i]) / 255
		}
		return cs.base.colorFromComponents(components)
	case "Separation", "DeviceN":
		if cs.tint == nil || cs.base == nil || len(values) < cs.components {
			return pdfColor{}, false
		}
		output, ok := cs.tint.eval(values[:cs.components])
		if !ok {
			return pdfColor{}, false
		}
		return cs.base.colorFromComponents(output)
	}

	if len(values) < cs.components {
		return pdfColor{}, false
	}
	switch cs.components {
	case 1:
		gray := clamp01(values[0])
		return rgbColor(gray, gray, gray), true
	case 3:
		return rgbColor(values[0], values[1], values[2]), true
	case 4:
		return cmykColor(values[0], values[1], values[2], values[3]), true
	default:
		return pdfColor{}, false
	}
}

func dereferenceName(ctx *model.Context, object types.Object) (string, bool) {
	object, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return "", false
	}
	name, ok := object.(types.Name)
	if !ok {
		return "", false
	}
	return name.Value(), true
}

// pdfFunction 表示 PDF Type 0/2/3/4 函数，用于 Separation/DeviceN 的 tint 变换。
type pdfFunction struct {
	fnType int
	domain []float64
	rng    []float64
	// Type 0 采样函数。
	size    []int
	bits    int
	encode  []float64
	decode  []float64
	samples []byte
	order   int
	// Type 2 指数插值。
	c0, c1 []float64
	exp    float64
	// Type 3 拼接函数。
	funcs  []*pdfFunction
	bounds []float64
	enc3   []float64
	// Type 4 PostScript 计算函数。
	code []byte
}

func (p *pdfInterpreter) parseFunction(object types.Object, depth int) *pdfFunction {
	if depth > 8 {
		return nil
	}
	object, err := dereferencePDFObject(p.ctx, object)
	if err != nil || object == nil {
		return nil
	}
	switch value := object.(type) {
	case types.StreamDict:
		return p.parseFunctionStream(&value, depth)
	case *types.StreamDict:
		if value == nil {
			return nil
		}
		return p.parseFunctionStream(value, depth)
	case types.Dict:
		return p.parseFunctionDict(value, depth)
	default:
		return nil
	}
}

func (p *pdfInterpreter) parseFunctionStream(stream *types.StreamDict, depth int) *pdfFunction {
	fnType, _ := dereferencedPDFNumber(p.ctx, stream.Dict["FunctionType"])
	if err := stream.Decode(); err != nil {
		return nil
	}
	fn := &pdfFunction{fnType: int(fnType), domain: pdfNumberArray(p.ctx, stream.Dict["Domain"]), rng: pdfNumberArray(p.ctx, stream.Dict["Range"]), samples: append([]byte(nil), stream.Content...)}
	switch fn.fnType {
	case 0:
		fn.size = pdfIntArray(p.ctx, stream.Dict["Size"])
		if bits, ok := dereferencedPDFNumber(p.ctx, stream.Dict["BitsPerSample"]); ok {
			fn.bits = int(bits)
		}
		fn.encode = pdfNumberArray(p.ctx, stream.Dict["Encode"])
		fn.decode = pdfNumberArray(p.ctx, stream.Dict["Decode"])
		if order, ok := dereferencedPDFNumber(p.ctx, stream.Dict["Order"]); ok {
			fn.order = int(order)
		}
		if len(fn.size) == 0 || fn.bits <= 0 {
			return nil
		}
	case 4:
		// PostScript 计算函数暂不支持求值，保留原始代码以便降级处理。
	default:
		return nil
	}
	return fn
}

func (p *pdfInterpreter) parseFunctionDict(dict types.Dict, depth int) *pdfFunction {
	fnType, _ := dereferencedPDFNumber(p.ctx, dict["FunctionType"])
	fn := &pdfFunction{fnType: int(fnType), domain: pdfNumberArray(p.ctx, dict["Domain"]), rng: pdfNumberArray(p.ctx, dict["Range"])}
	switch fn.fnType {
	case 2:
		fn.c0 = pdfNumberArray(p.ctx, dict["C0"])
		fn.c1 = pdfNumberArray(p.ctx, dict["C1"])
		fn.exp = 1
		if n, ok := dereferencedPDFNumber(p.ctx, dict["N"]); ok {
			fn.exp = n
		}
		if len(fn.c0) == 0 {
			fn.c0 = []float64{0}
		}
		if len(fn.c1) == 0 {
			fn.c1 = []float64{1}
		}
	case 3:
		functions, err := dereferencePDFObject(p.ctx, dict["Functions"])
		if err != nil {
			return nil
		}
		list, ok := functions.(types.Array)
		if !ok {
			return nil
		}
		for _, item := range list {
			sub := p.parseFunction(item, depth+1)
			if sub == nil {
				return nil
			}
			fn.funcs = append(fn.funcs, sub)
		}
		fn.bounds = pdfNumberArray(p.ctx, dict["Bounds"])
		fn.enc3 = pdfNumberArray(p.ctx, dict["Encode"])
		if len(fn.funcs) == 0 || len(fn.bounds) != len(fn.funcs)-1 {
			return nil
		}
	default:
		return nil
	}
	return fn
}

// eval 计算函数在输入点的输出。失败时返回 false，调用方应保留上一个颜色。
func (f *pdfFunction) eval(input []float64) ([]float64, bool) {
	if f == nil {
		return nil, false
	}
	inputs := make([]float64, len(input))
	copy(inputs, input)
	for i := range inputs {
		if 2*i+1 < len(f.domain) {
			inputs[i] = math.Max(f.domain[2*i], math.Min(f.domain[2*i+1], inputs[i]))
		}
	}
	switch f.fnType {
	case 0:
		return f.evalSampled(inputs)
	case 2:
		return f.evalExponential(inputs)
	case 3:
		return f.evalStitching(inputs)
	default:
		return nil, false
	}
}

func (f *pdfFunction) evalExponential(inputs []float64) ([]float64, bool) {
	if len(inputs) == 0 {
		return nil, false
	}
	outputs := make([]float64, len(f.c0))
	for i := range outputs {
		c1 := f.c1[min(i, len(f.c1)-1)]
		outputs[i] = f.c0[i] + math.Pow(inputs[0], f.exp)*(c1-f.c0[i])
	}
	return outputs, true
}

func (f *pdfFunction) evalStitching(inputs []float64) ([]float64, bool) {
	if len(inputs) == 0 {
		return nil, false
	}
	x := inputs[0]
	index := 0
	for index < len(f.bounds) && x >= f.bounds[index] {
		index++
	}
	if index >= len(f.funcs) {
		return nil, false
	}
	low := 0.0
	if index > 0 {
		low = f.bounds[index-1]
	}
	high := 1.0
	if index < len(f.bounds) {
		high = f.bounds[index]
	}
	encoded := x
	if high > low {
		encoded = (x - low) / (high - low)
	}
	if 2*index+1 < len(f.enc3) {
		encoded = f.enc3[2*index] + encoded*(f.enc3[2*index+1]-f.enc3[2*index])
	}
	return f.funcs[index].eval([]float64{encoded})
}

func (f *pdfFunction) evalSampled(inputs []float64) ([]float64, bool) {
	dimensions := len(f.size)
	if dimensions == 0 || f.bits <= 0 {
		return nil, false
	}
	outputs := len(f.rng) / 2
	if outputs == 0 {
		return nil, false
	}
	// 按 Encode 把输入映射到 [0, Size-1] 的采样坐标。
	coordinates := make([]float64, dimensions)
	for i := 0; i < dimensions; i++ {
		value := 0.0
		if i < len(inputs) {
			value = inputs[i]
		}
		if 2*i+1 < len(f.domain) && f.domain[2*i+1] > f.domain[2*i] {
			value = (value - f.domain[2*i]) / (f.domain[2*i+1] - f.domain[2*i])
		}
		encoded := value
		if 2*i+1 < len(f.encode) {
			encoded = f.encode[2*i] + value*(f.encode[2*i+1]-f.encode[2*i])
		}
		coordinates[i] = encoded * float64(f.size[i]-1)
	}
	// 按 order 1 做多线性插值；order 3 退化到最近邻采样。
	if f.order == 3 {
		for i := range coordinates {
			coordinates[i] = math.Round(coordinates[i])
		}
	}
	values := make([]float64, outputs)
	for out := 0; out < outputs; out++ {
		sample, ok := f.interpolate(coordinates, out)
		if !ok {
			return nil, false
		}
		value := sample
		if 2*out+1 < len(f.decode) {
			value = f.decode[2*out] + sample*(f.decode[2*out+1]-f.decode[2*out])
		}
		values[out] = value
	}
	return values, true
}

// interpolate 在多维采样表中对输出分量做线性插值。
func (f *pdfFunction) interpolate(coordinates []float64, output int) (float64, bool) {
	dimensions := len(f.size)
	low := make([]int, dimensions)
	high := make([]int, dimensions)
	fraction := make([]float64, dimensions)
	for i := 0; i < dimensions; i++ {
		value := math.Max(0, math.Min(float64(f.size[i]-1), coordinates[i]))
		lo := int(math.Floor(value))
		hi := lo + 1
		if hi > f.size[i]-1 {
			hi = f.size[i] - 1
		}
		low[i], high[i] = lo, hi
		if hi > lo {
			fraction[i] = value - float64(lo)
		}
	}
	// 2^dimensions 个角点，dimensions 通常为 1-4。
	corners := 1 << dimensions
	if corners > 256 {
		return 0, false
	}
	total := 0.0
	for corner := 0; corner < corners; corner++ {
		index := 0
		weight := 1.0
		for i := 0; i < dimensions; i++ {
			coordinate := low[i]
			if corner&(1<<i) != 0 {
				coordinate = high[i]
				weight *= fraction[i]
			} else {
				weight *= 1 - fraction[i]
			}
			index = index*f.size[i] + coordinate
		}
		sample, ok := f.sampleAt(index, output)
		if !ok {
			return 0, false
		}
		total += weight * sample
	}
	return total, true
}

func (f *pdfFunction) sampleAt(index, output int) (float64, bool) {
	outputs := len(f.rng) / 2
	if outputs == 0 {
		return 0, false
	}
	position := index*outputs + output
	switch f.bits {
	case 8:
		if position >= len(f.samples) {
			return 0, false
		}
		return float64(f.samples[position]) / 255, true
	case 16:
		offset := position * 2
		if offset+1 >= len(f.samples) {
			return 0, false
		}
		return float64(binary.BigEndian.Uint16(f.samples[offset:offset+2])) / 65535, true
	case 1, 2, 4:
		bit := position * f.bits
		byteIndex := bit / 8
		if byteIndex >= len(f.samples) {
			return 0, false
		}
		shift := 8 - f.bits - bit%8
		mask := byte((1 << f.bits) - 1)
		return float64((f.samples[byteIndex]>>uint(shift))&mask) / float64(mask), true
	case 12:
		bit := position * 12
		byteIndex := bit / 8
		if byteIndex+1 >= len(f.samples) {
			return 0, false
		}
		value := uint16(f.samples[byteIndex])<<8 | uint16(f.samples[byteIndex+1])
		return float64((value>>uint(4-bit%8))&0x0FFF) / 4095, true
	default:
		return 0, false
	}
}

func pdfNumberArray(ctx *model.Context, object types.Object) []float64 {
	object, err := dereferencePDFObject(ctx, object)
	if err != nil {
		return nil
	}
	array, ok := object.(types.Array)
	if !ok {
		return nil
	}
	values := make([]float64, 0, len(array))
	for _, item := range array {
		if value, ok := dereferencedPDFNumber(ctx, item); ok {
			values = append(values, value)
		} else {
			return nil
		}
	}
	return values
}

func pdfIntArray(ctx *model.Context, object types.Object) []int {
	values := pdfNumberArray(ctx, object)
	result := make([]int, len(values))
	for i, value := range values {
		result[i] = int(value)
	}
	return result
}

// colorFromOperands 按当前颜色空间解释 sc/scn/SC/SCN 操作数。颜色空间未知
// 时退回按分量个数猜测（1=灰、3=RGB、4=CMYK），保持历史行为。
func (p *pdfInterpreter) colorFromOperands(space *pdfColorSpace, args []any) (pdfColor, bool) {
	if space != nil {
		values := make([]float64, 0, len(args))
		for _, arg := range args {
			if _, ok := arg.(pdfName); ok {
				// 图案颜色空间的名称分量无法映射到 RGB，忽略。
				continue
			}
			values = append(values, anyFloat(arg))
		}
		if len(values) == 0 {
			return pdfColor{}, false
		}
		return space.colorFromComponents(values)
	}
	return pdfColorFromComponents(args)
}

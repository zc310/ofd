package pdf2ofd

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// repairPDFXRef 为原始 xref 空闲链表元数据会导致 pdfcpu panic 的 PDF 重建
// 经典 xref 表。对象正文原样复制。
func repairPDFXRef(data []byte) ([]byte, error) {
	options, _ := (Options{}).Normalize()
	return repairPDFXRefWithLimits(data, options.Limits)
}

func repairPDFXRefWithLimits(data []byte, limits Limits, controls ...*conversionControl) ([]byte, error) {
	options, _ := (Options{Limits: limits}).Normalize()
	control := &conversionControl{ctx: context.Background(), options: options}
	if len(controls) > 0 {
		control = controls[0]
	}
	if err := control.check(); err != nil {
		return nil, err
	}
	matches := pdfObjectHeader.FindAllIndex(data, int(limits.MaxObjects)+1)
	if int64(len(matches)) > limits.MaxObjects {
		return nil, &LimitError{Resource: "objects", Actual: int64(len(matches)), Limit: limits.MaxObjects}
	}
	if len(matches) == 0 {
		return nil, errors.New("PDF 没有可修复的间接对象")
	}
	objects := make([]pdfIndirectObject, 0, len(matches))
	for index, match := range matches {
		if err := control.check(); err != nil {
			return nil, err
		}
		header := pdfObjectHeader.FindSubmatch(data[match[0]:match[1]])
		if len(header) != 3 {
			continue
		}
		number, numberErr := strconv.Atoi(string(header[1]))
		generation, generationErr := strconv.Atoi(string(header[2]))
		if numberErr != nil || generationErr != nil || number <= 0 || generation < 0 {
			continue
		}
		if int64(number) >= limits.MaxObjects {
			return nil, &LimitError{Resource: "objects", Actual: int64(number) + 1, Limit: limits.MaxObjects}
		}
		end := len(data)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		objects = append(objects, pdfIndirectObject{number: number, generation: generation, data: bytes.TrimSpace(data[match[0]:end])})
	}
	if len(objects) == 0 {
		return nil, errors.New("PDF 没有可修复的有效间接对象")
	}
	byNumber := map[int]pdfIndirectObject{}
	for _, object := range objects {
		if current, exists := byNumber[object.number]; !exists || object.generation >= current.generation {
			byNumber[object.number] = object
		}
	}
	for _, object := range objects {
		extracted, ok, err := extractPDFObjectStreamLimited(object, limits, control)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, value := range extracted {
			if int64(value.number) >= limits.MaxObjects {
				return nil, &LimitError{Resource: "objects", Actual: int64(value.number) + 1, Limit: limits.MaxObjects}
			}
			if current, exists := byNumber[value.number]; !exists || value.generation >= current.generation {
				byNumber[value.number] = value
			}
		}
	}
	numbers := make([]int, 0, len(byNumber))
	for number := range byNumber {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	root := pdfRootReference.FindStringSubmatch(string(data))
	if len(root) != 3 {
		return nil, errors.New("PDF 缺少根对象")
	}
	rootNumber, rootErr := strconv.Atoi(root[1])
	rootGeneration, generationErr := strconv.Atoi(root[2])
	if rootErr != nil || generationErr != nil {
		return nil, errors.New("PDF 根对象无效")
	}
	if _, ok := byNumber[rootNumber]; !ok {
		return nil, errors.New("PDF 根对象不存在")
	}
	maxNumber := numbers[len(numbers)-1]
	var repaired bytes.Buffer
	repaired.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make(map[int]int, len(numbers))
	for _, number := range numbers {
		if err := control.check(); err != nil {
			return nil, err
		}
		if int64(len(byNumber[number].data))+1 > limits.MaxInputBytes-int64(repaired.Len()) {
			return nil, &LimitError{Resource: "repaired_input_bytes", Actual: limits.MaxInputBytes + 1, Limit: limits.MaxInputBytes}
		}
		offsets[number] = repaired.Len()
		repaired.Write(byNumber[number].data)
		repaired.WriteByte('\n')
	}
	xrefOffset := repaired.Len()
	// xref 每条固定 20 字节；先预留尾部元数据，避免稀疏编号把修复结果撑大。
	xrefBytes := int64(maxNumber+1)*20 + 256
	if xrefBytes > limits.MaxInputBytes-int64(repaired.Len()) {
		return nil, &LimitError{Resource: "repaired_input_bytes", Actual: limits.MaxInputBytes + 1, Limit: limits.MaxInputBytes}
	}
	fmt.Fprintf(&repaired, "xref\n0 %d\n0000000000 65535 f \n", maxNumber+1)
	for number := 1; number <= maxNumber; number++ {
		if err := control.check(); err != nil {
			return nil, err
		}
		object, ok := byNumber[number]
		if !ok {
			fmt.Fprint(&repaired, "0000000000 65535 f \n")
			continue
		}
		fmt.Fprintf(&repaired, "%010d %05d n \n", offsets[number], object.generation)
	}
	fmt.Fprintf(&repaired, "trailer\n<< /Size %d /Root %d %d R >>\nstartxref\n%d\n%%%%EOF\n", maxNumber+1, rootNumber, rootGeneration, xrefOffset)
	return repaired.Bytes(), nil
}

func extractPDFObjectStream(object pdfIndirectObject) ([]pdfIndirectObject, bool) {
	options, _ := (Options{}).Normalize()
	objects, ok, _ := extractPDFObjectStreamLimited(object, options.Limits)
	return objects, ok
}

func extractPDFObjectStreamLimited(object pdfIndirectObject, limits Limits, controls ...*conversionControl) ([]pdfIndirectObject, bool, error) {
	if !bytes.Contains(object.data, []byte("/Type /ObjStm")) {
		return nil, false, nil
	}
	first := pdfIntegerEntry(object.data, "/First")
	number := pdfIntegerEntry(object.data, "/N")
	if first < 0 || number <= 0 {
		return nil, false, nil
	}
	if int64(number) > limits.MaxObjects {
		return nil, false, &LimitError{Resource: "object_stream_objects", Actual: int64(number), Limit: limits.MaxObjects}
	}
	streamStart := bytes.Index(object.data, []byte("stream"))
	streamEnd := bytes.LastIndex(object.data, []byte("endstream"))
	if streamStart < 0 || streamEnd <= streamStart {
		return nil, false, nil
	}
	encoded := bytes.TrimLeft(object.data[streamStart+len("stream"):streamEnd], " \t\r\n")
	if int64(len(encoded)) > limits.MaxStreamBytes {
		return nil, false, &LimitError{Resource: "stream_bytes", Actual: int64(len(encoded)), Limit: limits.MaxStreamBytes}
	}
	maximum := limits.MaxDecodedStreamBytes
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
		if remaining := limits.MaxTotalDecodedBytes - control.decoded; remaining < maximum {
			maximum = remaining
		}
	}
	reader, err := zlib.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, false, nil
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	_ = reader.Close()
	if int64(len(decoded)) > limits.MaxDecodedStreamBytes {
		return nil, false, &LimitError{Resource: "decoded_stream_bytes", Actual: int64(len(decoded)), Limit: limits.MaxDecodedStreamBytes}
	}
	if control != nil {
		if int64(len(decoded)) > maximum {
			return nil, false, control.limit("total_decoded_bytes", limits.MaxTotalDecodedBytes+1, limits.MaxTotalDecodedBytes)
		}
		control.decoded += int64(len(decoded))
	}
	if err != nil || first > len(decoded) {
		return nil, false, nil
	}
	fields := strings.Fields(string(decoded[:first]))
	if number > len(fields)/2 {
		return nil, false, nil
	}
	result := make([]pdfIndirectObject, 0, number)
	for index := 0; index < number; index++ {
		objectNumber, numberErr := strconv.Atoi(fields[index*2])
		offset, offsetErr := strconv.Atoi(fields[index*2+1])
		if numberErr != nil || offsetErr != nil || offset < 0 {
			continue
		}
		if offset > len(decoded)-first {
			continue
		}
		start := first + offset
		end := len(decoded)
		if index+1 < number {
			next, nextErr := strconv.Atoi(fields[(index+1)*2+1])
			if nextErr == nil && next >= offset && next <= len(decoded)-first {
				end = first + next
			}
		}
		if start > len(decoded) || end > len(decoded) || start > end {
			continue
		}
		body := bytes.TrimSpace(decoded[start:end])
		result = append(result, pdfIndirectObject{number: objectNumber, data: []byte(fmt.Sprintf("%d 0 obj\n%s\nendobj", objectNumber, body))})
	}
	return result, len(result) > 0, nil
}

func pdfIntegerEntry(data []byte, key string) int {
	pattern := regexp.MustCompile(regexp.QuoteMeta(key) + `\s+(-?\d+)`)
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		return -1
	}
	value, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return -1
	}
	return value
}

func numberValue(object types.Object) (float64, bool) {
	switch value := object.(type) {
	case types.Float:
		return float64(value), true
	case types.Integer:
		return float64(value), true
	default:
		return 0, false
	}
}

func integerValue(object types.Object) (int, bool) {
	switch value := object.(type) {
	case types.Integer:
		return int(value), true
	case types.Float:
		return int(value), true
	default:
		return 0, false
	}
}

func dereferencedSubDict(ctx *model.Context, resources types.Dict, key string) (types.Dict, bool) {
	object, found := resources.Find(key)
	if !found {
		return nil, false
	}
	value, err := ctx.XRefTable.DereferenceDict(object)
	return value, err == nil && value != nil
}

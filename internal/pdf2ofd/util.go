package pdf2ofd

import (
	"bytes"
	"compress/zlib"
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
	matches := pdfObjectHeader.FindAllIndex(data, -1)
	if len(matches) == 0 {
		return nil, errors.New("PDF 没有可修复的间接对象")
	}
	objects := make([]pdfIndirectObject, 0, len(matches))
	for index, match := range matches {
		header := pdfObjectHeader.FindSubmatch(data[match[0]:match[1]])
		if len(header) != 3 {
			continue
		}
		number, numberErr := strconv.Atoi(string(header[1]))
		generation, generationErr := strconv.Atoi(string(header[2]))
		if numberErr != nil || generationErr != nil || number <= 0 || generation < 0 {
			continue
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
		extracted, ok := extractPDFObjectStream(object)
		if !ok {
			continue
		}
		for _, value := range extracted {
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
		offsets[number] = repaired.Len()
		repaired.Write(byNumber[number].data)
		repaired.WriteByte('\n')
	}
	xrefOffset := repaired.Len()
	fmt.Fprintf(&repaired, "xref\n0 %d\n0000000000 65535 f \n", maxNumber+1)
	for number := 1; number <= maxNumber; number++ {
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
	if !bytes.Contains(object.data, []byte("/Type /ObjStm")) {
		return nil, false
	}
	first := pdfIntegerEntry(object.data, "/First")
	number := pdfIntegerEntry(object.data, "/N")
	if first < 0 || number <= 0 {
		return nil, false
	}
	streamStart := bytes.Index(object.data, []byte("stream"))
	streamEnd := bytes.LastIndex(object.data, []byte("endstream"))
	if streamStart < 0 || streamEnd <= streamStart {
		return nil, false
	}
	encoded := bytes.TrimLeft(object.data[streamStart+len("stream"):streamEnd], " \t\r\n")
	reader, err := zlib.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, false
	}
	decoded, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || first > len(decoded) {
		return nil, false
	}
	fields := strings.Fields(string(decoded[:first]))
	if len(fields) < number*2 {
		return nil, false
	}
	result := make([]pdfIndirectObject, 0, number)
	for index := 0; index < number; index++ {
		objectNumber, numberErr := strconv.Atoi(fields[index*2])
		offset, offsetErr := strconv.Atoi(fields[index*2+1])
		if numberErr != nil || offsetErr != nil || offset < 0 {
			continue
		}
		start := first + offset
		end := len(decoded)
		if index+1 < number {
			next, nextErr := strconv.Atoi(fields[(index+1)*2+1])
			if nextErr == nil && next >= offset {
				end = first + next
			}
		}
		if start > len(decoded) || end > len(decoded) || start > end {
			continue
		}
		body := bytes.TrimSpace(decoded[start:end])
		result = append(result, pdfIndirectObject{number: objectNumber, data: []byte(fmt.Sprintf("%d 0 obj\n%s\nendobj", objectNumber, body))})
	}
	return result, len(result) > 0
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

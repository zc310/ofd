package ocr

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Tesseract 调用系统中的 tesseract 命令行程序。
type Tesseract struct {
	Path     string
	Language string
	PSM      int
	TempDir  string
}

// NewTesseract 创建 Tesseract OCR 引擎。
func NewTesseract(path, language string) *Tesseract {
	return &Tesseract{Path: strings.TrimSpace(path), Language: strings.TrimSpace(language)}
}

// Recognize 执行 OCR，并把 TSV 单词结果聚合为文字行。
func (t *Tesseract) Recognize(ctx context.Context, input image.Image) ([]TextBlock, error) {
	if input == nil {
		return nil, fmt.Errorf("OCR 输入图片为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(t.TempDir, "ofd-ocr-*.png")
	if err != nil {
		return nil, fmt.Errorf("创建 OCR 临时文件失败: %w", err)
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err := png.Encode(file, input); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("编码 OCR 输入图片失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("关闭 OCR 临时文件失败: %w", err)
	}
	path := t.Path
	if path == "" {
		path = "tesseract"
	}
	language := t.Language
	if language == "" {
		language = "chi_sim+eng"
	}
	psm := t.PSM
	if psm <= 0 {
		psm = 3
	}
	command := exec.CommandContext(ctx, path, name, "stdout", "--psm", strconv.Itoa(psm), "-l", language, "tsv")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("Tesseract OCR 失败: %s", message)
	}
	blocks, err := ParseTSV(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		return nil, err
	}
	return Normalize(blocks, input.Bounds()), nil
}

// ParseTSV 解析 Tesseract TSV，仅保留 word 层并按 page/block/paragraph/line 聚合。
func ParseTSV(input io.Reader) ([]TextBlock, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	type lineKey struct{ page, block, paragraph, line int }
	type lineValue struct {
		words      []string
		bounds     image.Rectangle
		confidence float64
		count      int
	}
	lines := make(map[lineKey]*lineValue)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 12 || fields[0] == "level" {
			continue
		}
		level, err := tsvInt(fields[0])
		if err != nil {
			return nil, fmt.Errorf("解析 Tesseract TSV level 失败: %w", err)
		}
		if level != 5 {
			continue
		}
		values := make([]int, 8)
		for i, fieldIndex := range []int{1, 2, 3, 4, 6, 7, 8, 9} {
			values[i], err = tsvInt(fields[fieldIndex])
			if err != nil {
				return nil, fmt.Errorf("解析 Tesseract TSV 字段 %d 失败: %w", fieldIndex, err)
			}
		}
		text := strings.TrimSpace(strings.Join(fields[11:], "\t"))
		left, top, width, height := values[4], values[5], values[6], values[7]
		if text == "" || width <= 0 || height <= 0 {
			continue
		}
		confidence, err := strconv.ParseFloat(strings.TrimSpace(fields[10]), 64)
		if err != nil || confidence < 0 {
			continue
		}
		key := lineKey{page: values[0], block: values[1], paragraph: values[2], line: values[3]}
		value := lines[key]
		wordBounds := image.Rect(left, top, left+width, top+height)
		if value == nil {
			value = &lineValue{bounds: wordBounds}
			lines[key] = value
		} else {
			value.bounds = value.bounds.Union(wordBounds)
		}
		value.words = append(value.words, text)
		value.confidence += confidence
		value.count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 Tesseract TSV 失败: %w", err)
	}
	result := make([]TextBlock, 0, len(lines))
	for _, value := range lines {
		result = append(result, TextBlock{Text: JoinWords(value.words), Bounds: value.bounds, Confidence: value.confidence / float64(value.count)})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Bounds.Min.Y != result[j].Bounds.Min.Y {
			return result[i].Bounds.Min.Y < result[j].Bounds.Min.Y
		}
		return result[i].Bounds.Min.X < result[j].Bounds.Min.X
	})
	return result, nil
}

func tsvInt(value string) (int, error) { return strconv.Atoi(strings.TrimSpace(value)) }

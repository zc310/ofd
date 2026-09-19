package creator

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
)

type memorySource struct {
	data  []byte
	opens int
}

func (s *memorySource) Open() (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func (s *memorySource) Size() int64 { return int64(len(s.data)) }

func openZipEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("解析生成的 OFD ZIP 失败: %v", err)
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("打开条目 %q 失败: %v", file.Name, err)
		}
		content, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatalf("读取条目 %q 失败: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	return entries
}

func findEntryByPrefix(entries map[string][]byte, prefix string) (string, []byte) {
	for name, data := range entries {
		if strings.HasPrefix(name, prefix) {
			return name, data
		}
	}
	return "", nil
}

func TestCreateStreamsImageSource(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9, 8, 7, 6}
	source := &memorySource{data: png}
	document := Document{
		ID:       "source-image",
		PageSize: A4,
		Pages: []Page{{
			Items: []Item{Image{X: 1, Y: 2, Width: 3, Height: 4, Source: source}},
		}},
	}
	data, err := Marshal(document)
	if err != nil {
		t.Fatalf("创建带惰性图片来源的 OFD 失败: %v", err)
	}
	entries := openZipEntries(t, data)
	name, content := findEntryByPrefix(entries, "Doc_0/Res/Images/")
	if name == "" {
		t.Fatalf("未找到图片资源条目: %v", entries)
	}
	if !bytes.Equal(content, png) {
		t.Fatalf("图片资源内容与来源不一致")
	}
	if source.opens < 2 {
		t.Fatalf("图片来源应被多次打开（摘要/写入），实际 %d 次", source.opens)
	}
}

func TestCreateStreamsBinaryResourcesFromFiles(t *testing.T) {
	dir := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	writeFile := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}
		return path
	}
	pageImagePath := writeFile("page.png", png)
	imagePath := writeFile("image.png", append(append([]byte{}, png...), 1))
	mediaPath := writeFile("sound.wav", []byte("RIFF....WAVE"))
	attachmentPath := writeFile("attach.bin", []byte("attachment-bytes"))
	coverPath := writeFile("cover.png", append(append([]byte{}, png...), 2))

	document := Document{
		ID:          "source-files",
		PageSize:    A4,
		Cover:       "cover.png",
		CoverSource: FileDataSource(coverPath),
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{
				ID: 1, Format: "PNG", Name: "page.png", Source: FileDataSource(pageImagePath),
			}}}},
			Items: []Item{Image{X: 1, Y: 1, Width: 2, Height: 2, Source: FileDataSource(imagePath)}},
		}},
		Media: []Media{{
			ID: 2000, Type: "Audio", Format: "wav", Name: "sound.wav", Source: FileDataSource(mediaPath),
		}},
		Attachments: []Attachment{{
			ID: "attach", Name: "附件", FileName: "attach.bin", Source: FileDataSource(attachmentPath),
		}},
	}
	data, err := Marshal(document)
	if err != nil {
		t.Fatalf("创建带文件来源的 OFD 失败: %v", err)
	}
	entries := openZipEntries(t, data)
	for _, expected := range []struct {
		prefix  string
		content []byte
	}{
		{"Doc_0/Pages/Page_0/Images/", png},
		{"Doc_0/Res/Media/", []byte("RIFF....WAVE")},
		{"Doc_0/Attachments/Files/", []byte("attachment-bytes")},
		{"Doc_0/Cover/", append(append([]byte{}, png...), 2)},
	} {
		_, content := findEntryByPrefix(entries, expected.prefix)
		if content == nil {
			t.Fatalf("未找到前缀 %q 的条目: %v", expected.prefix, entries)
		}
		if !bytes.Equal(content, expected.content) {
			t.Fatalf("前缀 %q 条目内容与来源文件不一致", expected.prefix)
		}
	}
}

func TestCreateStreamsRemainingResourceSources(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}
		return path
	}
	sharedPath := write("shared.bin", []byte("shared-file"))
	pageFilePath := write("page-file.bin", []byte("page-file"))
	fontPath := write("font.ttf", []byte("font-bytes"))
	extPath := write("ext.bin", []byte("extension-file"))
	sealPath := write("seal.bin", []byte("seal-file"))

	publicXML := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio" Format="wav"><MediaFile>data.bin</MediaFile></MultiMedia></MultiMedias></Res>`)
	pageXML := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="71" Type="Audio" Format="wav"><MediaFile>page-file.bin</MediaFile></MultiMedia></MultiMedias></Res>`)
	document := Document{
		ID:       "source-remaining",
		PageSize: A4,
		PublicRes: []PublicResource{{
			Name: "Assets/SharedRes.xml", Data: publicXML,
			Files: []PublicResourceFile{{Path: "data.bin", Source: FileDataSource(sharedPath)}},
		}},
		Fonts: []Font{{Name: "TestFont", Format: "ttf", Source: FileDataSource(fontPath)}},
		Pages: []Page{{
			Resources: []PageResource{{
				Data:  pageXML,
				Files: []PageResourceFile{{Path: "page-file.bin", Source: FileDataSource(pageFilePath)}},
			}},
		}},
		Extensions: []Extension{{AppName: "app", RefID: 70, DataName: "ext.bin", DataFileSource: FileDataSource(extPath)}},
		Signatures: []Signature{{
			ID: "sig1", Type: "Seal", ProviderName: "p", Date: time.Unix(0, 0).UTC(),
			SealSource: FileDataSource(sealPath),
			References: []SignatureReference{{FileRef: "../Document.xml"}},
		}},
	}
	data, err := MarshalWithOptions(document, CreateOptions{Compression: CompressionAuto, PreserveEmbeddedFonts: true})
	if err != nil {
		t.Fatalf("创建带剩余惰性来源的 OFD 失败: %v", err)
	}
	entries := openZipEntries(t, data)
	for _, expected := range []struct {
		prefix  string
		content []byte
	}{
		{"Doc_0/Assets/data.bin", []byte("shared-file")},
		{"Doc_0/Pages/Page_0/page-file.bin", []byte("page-file")},
		{"Doc_0/Res/Fonts/", []byte("font-bytes")},
		{"Doc_0/Extensions/Data/ext.bin", []byte("extension-file")},
		{"Doc_0/Signatures/Data/", []byte("seal-file")},
	} {
		_, content := findEntryByPrefix(entries, expected.prefix)
		if content == nil {
			t.Fatalf("未找到前缀 %q 的条目: %v", expected.prefix, entries)
		}
		if !bytes.Equal(content, expected.content) {
			t.Fatalf("前缀 %q 条目内容与来源文件不一致", expected.prefix)
		}
	}
}

func TestCreateStreamsSourceLargerThanHeap(t *testing.T) {
	const resourceSize = 64 << 20
	source := zeroDataSource{size: resourceSize}
	document := Document{
		ID:       "large-source",
		PageSize: A4,
		Pages: []Page{{
			Items: []Item{Image{X: 1, Y: 1, Width: 2, Height: 2, Format: "PNG", Source: source}},
		}},
	}

	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := Create(document, io.Discard); err != nil {
		t.Fatalf("流式创建大资源 OFD 失败: %v", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if delta > resourceSize/4 {
		t.Fatalf("流式写入 %d 字节资源时额外常驻内存过大: %d 字节", resourceSize, delta)
	}
}

type zeroDataSource struct{ size int64 }

func (s zeroDataSource) Open() (io.ReadCloser, error) {
	return io.NopCloser(io.LimitReader(zeroReader{}, s.size)), nil
}

func (s zeroDataSource) Size() int64 { return s.size }

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

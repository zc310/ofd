package parser

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/ses"
)

// buildSealPackage 以 999.ofd 为基础构造测试用 OFD 包。
//
// sealElement 非空时替换 Signature.xml 中的签名元素（调用方自行拼好
// <ofd:Seal><ofd:BaseLoc>…</ofd:BaseLoc></ofd:Seal>）；extra 里的条目按原样写入
// 包内。999.ofd 本身带 StampAnnot 和内嵌印章的 SignedValue.dat，正好用来验证
// 独立印章文件缺失时的回退行为。
func buildSealPackage(t *testing.T, sealElement string, extra map[string][]byte) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range reader.File {
		if file.Name == "Doc_0/Signs/Sign_0/Signature.xml" && sealElement != "" {
			content := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), file.Name)
			text := string(content)
			// Seal 必须落在 SignedInfo 内（models.SignedInfo.Seal），
			// 插到 SignedValue 之前会被解析成 SignedInfo 的兄弟节点而读不到。
			if !strings.Contains(text, "</ofd:SignedInfo>") {
				t.Fatalf("Signature.xml 结构与预期不符:\n%s", text)
			}
			text = strings.Replace(text, "</ofd:SignedInfo>", sealElement+"</ofd:SignedInfo>", 1)
			if err := writeZipEntry(archive, file.Name, []byte(text)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		content := readZipEntry(t, filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"), file.Name)
		if err := writeZipEntry(archive, file.Name, content); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range extra {
		if err := writeZipEntry(archive, name, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writeZipEntry(archive *zip.Writer, name string, data []byte) error {
	writer, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// sealFileElement 拼出 Seal 元素；baseLoc 为空表示引用不存在的文件。
func sealFileElement(baseLoc string) string {
	return "<ofd:Seal><ofd:BaseLoc>" + baseLoc + "</ofd:BaseLoc></ofd:Seal>"
}

// TestLoadSealsFallsBackWhenSealFileMissing 保护独立印章文件缺失时的回退。
//
// 此前这里直接 return err：Seal@BaseLoc 指向一个不存在的文件，整份文档就解析
// 失败，转换、分析与阅读器全部不可用。签名本身可能仍然有效，一份坏印章不该
// 拖垮整份文档。
func TestLoadSealsFallsBackWhenSealFileMissing(t *testing.T) {
	data := buildSealPackage(t, sealFileElement("/Doc_0/Signs/Sign_0/Seal.esl"), nil)
	ofd, err := NewOFD(data)
	if err != nil {
		t.Fatalf("Seal 文件缺失时不应中止解析: %v", err)
	}
	defer func() { _ = ofd.Close() }()
	seals := ofd.Documents[0].GetSeals(models.StID(10))
	if len(seals) != 1 {
		t.Fatalf("印章数量 = %d, 期望回退到 SignedValue 内嵌印章的 1 枚", len(seals))
	}
	if seals[0].SealData == nil || len(seals[0].SealData.Data) == 0 {
		t.Fatal("回退后的印章数据为空")
	}
}

// TestLoadSealsFallsBackWhenSealFileUnreadable 独立印章文件存在但内容不是合法
// 印章时同样回退，不让这份签名的印章全部消失。
func TestLoadSealsFallsBackWhenSealFileUnreadable(t *testing.T) {
	extra := map[string][]byte{
		"Doc_0/Signs/Sign_0/Seal.esl": []byte("not-a-seal"),
	}
	data := buildSealPackage(t, sealFileElement("/Doc_0/Signs/Sign_0/Seal.esl"), extra)
	ofd, err := NewOFD(data)
	if err != nil {
		t.Fatalf("Seal 内容非法时不应中止解析: %v", err)
	}
	defer func() { _ = ofd.Close() }()
	seals := ofd.Documents[0].GetSeals(models.StID(10))
	if len(seals) != 1 || seals[0].SealData == nil || len(seals[0].SealData.Data) == 0 {
		t.Fatalf("印章应回退到 SignedValue 内嵌印章, 实际 %+v", seals)
	}
}

// TestLoadSealsPrefersSealFile 独立印章文件可用时优先用它，而不是 SignedValue
// 内嵌的那枚。这是渲染实际采用的口径，不能被回退逻辑意外改掉。
func TestLoadSealsPrefersSealFile(t *testing.T) {
	// 用一张与内嵌印章不同的图片构造独立印章：只有真正读了 Seal.esl 才会拿到它。
	// 名称必须用 ASCII：VID/ESID 在 GB/T 38540 里是 IA5String。
	independent := testSealFile(t, "independent-seal")
	extra := map[string][]byte{
		"Doc_0/Signs/Sign_0/Seal.esl": independent,
	}
	data := buildSealPackage(t, sealFileElement("/Doc_0/Signs/Sign_0/Seal.esl"), extra)
	ofd, err := NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	seals := ofd.Documents[0].GetSeals(models.StID(10))
	if len(seals) != 1 {
		t.Fatalf("印章数量 = %d, 期望 1", len(seals))
	}
	fromFile, err := ExtractSealData(independent)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(seals[0].SealData.Data, fromFile.Data) {
		t.Fatal("印章应取自 Seal.esl，而不是 SignedValue 内嵌印章")
	}
}

// testSealFile 造一份合法的独立印章文件，内容为纯色小图。
func testSealFile(t *testing.T, name string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 0xE6, A: 0xFF})
		}
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, img); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	key, _, certDER, err := ses.NewSelfSignedCertificate(name, "Seal Fallback Test", now)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := ses.BuildSeal(ses.SealParams{
		Provider:    "ofd-parser-test/" + name,
		ESID:        name + "@ofd-parser-test",
		Name:        name,
		PictureType: "png",
		PictureData: picture.Bytes(),
		Width:       2,
		Height:      2,
	}, certDER, key, now)
	if err != nil {
		t.Fatal(err)
	}
	der, err := seal.MarshalDER()
	if err != nil {
		t.Fatal(err)
	}
	return der
}

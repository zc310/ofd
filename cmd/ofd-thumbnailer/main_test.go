package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type zipTestEntry struct {
	name string
	data []byte
}

func writeTestArchive(t *testing.T, name string, entries ...zipTestEntry) string {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range entries {
		writer, err := archive.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testPNG(t *testing.T, pixel color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, pixel)
	img.SetRGBA(1, 0, pixel)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func readThumbnail(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, format, err := image.Decode(file)
	if err != nil || format != "png" {
		t.Fatalf("thumbnail format = %q, error = %v; want PNG", format, err)
	}
	return img
}

func assertNoTemporaryThumbnails(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".ofd-thumbnail-*.png"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("temporary thumbnails = %v, error = %v", paths, err)
	}
}

func TestParseSizeRejectsOutOfRangeAndOverflow(t *testing.T) {
	for _, text := range []string{"0", "-1", "4097", "not-a-size", strconv.Itoa(int(^uint(0) >> 1)), "999999999999999999999999"} {
		if size, err := parseSize(text); err == nil {
			t.Errorf("parseSize(%q) = %d, want error", text, size)
		}
	}
	for _, size := range []int{1, 128, maxThumbnailSize} {
		if got, err := parseSize(strconv.Itoa(size)); err != nil || got != size {
			t.Errorf("parseSize(%d) = %d, %v", size, got, err)
		}
	}
}

func TestRunZIPUsesFirstDecodableImageAndAlwaysWritesPNG(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	input := writeTestArchive(t, "images.zip",
		zipTestEntry{"notes.txt", []byte("not an image")},
		zipTestEntry{"broken.png", []byte("not a PNG")},
		zipTestEntry{"first.png", testPNG(t, red)},
		zipTestEntry{"second.png", testPNG(t, color.RGBA{B: 255, A: 255})},
	)
	for _, name := range []string{"thumbnail.png", "thumbnail", "thumbnail.jpg"} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), name)
			if err := run(context.Background(), []string{input, output, "16"}); err != nil {
				t.Fatal(err)
			}
			img := readThumbnail(t, output)
			if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 8 {
				t.Fatalf("thumbnail bounds = %v; want 16x8", img.Bounds())
			}
			if pixel := color.RGBAModel.Convert(img.At(8, 4)); pixel != red {
				t.Fatalf("thumbnail pixel = %v; want first decodable image", pixel)
			}
			assertNoTemporaryThumbnails(t, filepath.Dir(output))
		})
	}
}

func TestRunRejectsOutputAliasesWithoutChangingInput(t *testing.T) {
	for _, kind := range []string{"same-path", "hardlink", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			input := writeTestArchive(t, "input.zip", zipTestEntry{"cover.png", testPNG(t, color.RGBA{A: 255})})
			before, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "thumbnail.png")
			switch kind {
			case "same-path":
				output = input
			case "hardlink":
				if err := os.Link(input, output); err != nil {
					t.Skipf("hardlinks unavailable: %v", err)
				}
			case "symlink":
				if err := os.Symlink(input, output); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if err := run(context.Background(), []string{input, output, "16"}); !errors.Is(err, ErrInvalidArgs) {
				t.Fatalf("alias output error = %v; want ErrInvalidArgs", err)
			}
			after, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("input changed: error = %v", err)
			}
		})
	}
}

func writeTestOFD(t *testing.T, width, height float64) string {
	t.Helper()
	root := `<ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><ofd:DocBody><ofd:DocInfo><ofd:DocID>thumbnail-test</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`
	document := fmt.Sprintf(`<ofd:Document xmlns:ofd="http://www.ofdspec.org/2016"><ofd:CommonData><ofd:MaxUnitID>6</ofd:MaxUnitID><ofd:PageArea><ofd:PhysicalBox>0 0 %g %g</ofd:PhysicalBox></ofd:PageArea></ofd:CommonData><ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/First.xml"/><ofd:Page ID="4" BaseLoc="Pages/Second.xml"/></ofd:Pages></ofd:Document>`, width, height)
	page := func(layer, object int, fill string) []byte {
		return []byte(fmt.Sprintf(`<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Area><ofd:PhysicalBox>0 0 %g %g</ofd:PhysicalBox></ofd:Area><ofd:Content><ofd:Layer ID="%d"><ofd:PathObject ID="%d" Boundary="0 0 %g %g" Fill="true" Stroke="false"><ofd:FillColor Value="%s"/><ofd:AbbreviatedData>M 0 0 L %g 0 L %g %g L 0 %g C</ofd:AbbreviatedData></ofd:PathObject></ofd:Layer></ofd:Content></ofd:Page>`, width, height, layer, object, width, height, fill, width, width, height, height))
	}
	return writeTestArchive(t, "input.OFD",
		zipTestEntry{"OFD.xml", []byte(root)},
		zipTestEntry{"Doc_0/Document.xml", []byte(document)},
		zipTestEntry{"Doc_0/Pages/First.xml", page(2, 3, "255 0 0")},
		zipTestEntry{"Doc_0/Pages/Second.xml", page(5, 6, "0 0 255")},
	)
}

func TestRunOFDScalesFirstPageBeforeRasterizing(t *testing.T) {
	for _, dimensions := range [][2]float64{{200, 100}, {1e12, 1e12}} {
		t.Run(fmt.Sprint(dimensions), func(t *testing.T) {
			input := writeTestOFD(t, dimensions[0], dimensions[1])
			output := filepath.Join(t.TempDir(), "thumbnail")
			if err := run(context.Background(), []string{input, output, "64"}); err != nil {
				t.Fatal(err)
			}
			img := readThumbnail(t, output)
			wantHeight := int(64 * dimensions[1] / dimensions[0])
			if img.Bounds().Dx() != 64 || img.Bounds().Dy() != wantHeight {
				t.Fatalf("thumbnail bounds = %v; want 64x%d", img.Bounds(), wantHeight)
			}
			red, green, blue, alpha := img.At(32, wantHeight/2).RGBA()
			if red < 0xff00 || green != 0 || blue != 0 || alpha != 0xffff {
				t.Fatalf("first page center = %d,%d,%d,%d; want opaque red", red, green, blue, alpha)
			}
		})
	}
}

func TestThumbnailResolutionEnforcesPixelBudget(t *testing.T) {
	for _, dimensions := range [][2]float64{{210, 297}, {1e19, 1e19}, {math.MaxFloat64, math.MaxFloat64}} {
		resolution, err := thumbnailResolution(dimensions[0], dimensions[1], maxThumbnailSize)
		if err != nil {
			t.Fatal(err)
		}
		width := math.Floor(dimensions[0]*resolution.DPMM() + 0.5)
		height := math.Floor(dimensions[1]*resolution.DPMM() + 0.5)
		if width < 1 || height < 1 || width > maxThumbnailSize || height > maxThumbnailSize || width*height > maxRasterPixels {
			t.Fatalf("raster dimensions outside budget: %gx%g", width, height)
		}
	}
	for _, dimensions := range [][2]float64{{0, 10}, {-1, 10}, {math.NaN(), 10}, {10, math.Inf(1)}, {1e19, 10}, {math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64}} {
		if _, err := thumbnailResolution(dimensions[0], dimensions[1], 128); err == nil {
			t.Errorf("unsafe dimensions %v accepted", dimensions)
		}
	}
}

func TestRunRejectsUnsafeOFDPageWithoutCreatingOutput(t *testing.T) {
	input := writeTestOFD(t, 1e19, 10)
	output := filepath.Join(t.TempDir(), "thumbnail.png")
	if err := run(context.Background(), []string{input, output, "128"}); err == nil {
		t.Fatal("extreme page aspect ratio should return an error")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("unsafe page created output: %v", err)
	}
}

type cancellingImage struct {
	image.Image
	cancel context.CancelFunc
}

func (img cancellingImage) At(x, y int) color.Color {
	img.cancel()
	return img.Image.At(x, y)
}

func TestSaveThumbnailCancellationPreservesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.zip"), filepath.Join(dir, "thumbnail.png")
	if err := os.WriteFile(output, []byte("existing thumbnail"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	img := cancellingImage{Image: image.NewRGBA(image.Rect(0, 0, 2, 2)), cancel: cancel}
	if err := saveThumbnail(ctx, input, output, img); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "existing thumbnail" {
		t.Fatalf("existing output changed: %q, %v", data, err)
	}
	assertNoTemporaryThumbnails(t, dir)
}

func TestRunRejectsInvalidArgumentsAndCancelledContext(t *testing.T) {
	for _, args := range [][]string{nil, {"input.zip", "output.png"}, {"input.zip", "output.png", "4097"}} {
		if err := run(context.Background(), args); !errors.Is(err, ErrInvalidArgs) {
			t.Errorf("run(%v) = %v; want ErrInvalidArgs", args, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"missing.zip", "output.png", "128"}); err != context.Canceled {
		t.Fatalf("cancelled run = %v; want context.Canceled", err)
	}
}

//go:build openjpeg && cgo && linux

package pdf2ofd

/*
#cgo pkg-config: libopenjp2
#include <openjpeg.h>
#include <stdlib.h>

static void ofd_opj_silent(const char *message, void *data) {
	(void)message;
	(void)data;
}

static opj_image_t *ofd_opj_decode_file(const char *filename, OPJ_CODEC_FORMAT format) {
	opj_dparameters_t parameters;
	opj_set_default_decoder_parameters(&parameters);
	opj_codec_t *codec = opj_create_decompress(format);
	if (codec == NULL) return NULL;
	opj_set_info_handler(codec, ofd_opj_silent, NULL);
	opj_set_warning_handler(codec, ofd_opj_silent, NULL);
	opj_set_error_handler(codec, ofd_opj_silent, NULL);
	if (!opj_setup_decoder(codec, &parameters)) {
		opj_destroy_codec(codec);
		return NULL;
	}
	opj_codec_set_threads(codec, 1);
	opj_stream_t *stream = opj_stream_create_default_file_stream(filename, OPJ_TRUE);
	if (stream == NULL) {
		opj_destroy_codec(codec);
		return NULL;
	}
	opj_image_t *image = NULL;
	OPJ_BOOL ok = opj_read_header(stream, codec, &image);
	if (ok) ok = opj_decode(codec, stream, image);
	if (ok) ok = opj_end_decompress(codec, stream);
	opj_stream_destroy(stream);
	opj_destroy_codec(codec);
	if (!ok) {
		opj_image_destroy(image);
		return NULL;
	}
	return image;
}

static OPJ_UINT32 ofd_opj_width(opj_image_t *image) { return image->x1 - image->x0; }
static OPJ_UINT32 ofd_opj_height(opj_image_t *image) { return image->y1 - image->y0; }
static OPJ_UINT32 ofd_opj_components(opj_image_t *image) { return image->numcomps; }
static OPJ_UINT32 ofd_opj_component_width(opj_image_t *image, OPJ_UINT32 c) { return image->comps[c].w; }
static OPJ_UINT32 ofd_opj_component_height(opj_image_t *image, OPJ_UINT32 c) { return image->comps[c].h; }
static OPJ_UINT32 ofd_opj_precision(opj_image_t *image, OPJ_UINT32 c) { return image->comps[c].prec; }
static OPJ_BOOL ofd_opj_signed(opj_image_t *image, OPJ_UINT32 c) { return image->comps[c].sgnd; }
static OPJ_INT32 ofd_opj_sample(opj_image_t *image, OPJ_UINT32 c, OPJ_SIZE_T i) { return image->comps[c].data[i]; }
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
)

func decodeJPXOpenJPEG(data []byte) (*jpeg2000.FloatImage, error) {
	file, err := os.CreateTemp("", "ofd-jpx-*.jp2")
	if err != nil {
		return nil, fmt.Errorf("创建 OpenJPEG 临时输入失败: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("写入 OpenJPEG 临时输入失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("关闭 OpenJPEG 临时输入失败: %w", err)
	}
	filename := C.CString(name)
	defer C.free(unsafe.Pointer(filename))
	format := C.OPJ_CODEC_FORMAT(C.OPJ_CODEC_JP2)
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0x4f {
		format = C.OPJ_CODEC_J2K
	}
	decoded := C.ofd_opj_decode_file(filename, format)
	if decoded == nil {
		return nil, fmt.Errorf("OpenJPEG 无法解码 JPX 码流")
	}
	defer C.opj_image_destroy(decoded)

	width, height := int(C.ofd_opj_width(decoded)), int(C.ofd_opj_height(decoded))
	components := int(C.ofd_opj_components(decoded))
	if width <= 0 || height <= 0 || components <= 0 || components > 5 || width > int(^uint(0)>>1)/height {
		return nil, fmt.Errorf("OpenJPEG 返回了无效的图像尺寸或分量数")
	}
	result := &jpeg2000.FloatImage{
		Width: width, Height: height, Components: make([][]float32, components),
		BitDepth: int(C.ofd_opj_precision(decoded, 0)), Signed: C.ofd_opj_signed(decoded, 0) != 0,
	}
	pixels := width * height
	for c := 0; c < components; c++ {
		if int(C.ofd_opj_component_width(decoded, C.OPJ_UINT32(c))) != width || int(C.ofd_opj_component_height(decoded, C.OPJ_UINT32(c))) != height {
			return nil, fmt.Errorf("OpenJPEG returned subsampled JPX components")
		}
		if int(C.ofd_opj_precision(decoded, C.OPJ_UINT32(c))) != result.BitDepth || (C.ofd_opj_signed(decoded, C.OPJ_UINT32(c)) != 0) != result.Signed {
			return nil, fmt.Errorf("OpenJPEG returned JPX components with mixed precision")
		}
		plane := make([]float32, pixels)
		for i := range pixels {
			plane[i] = float32(C.ofd_opj_sample(decoded, C.OPJ_UINT32(c), C.OPJ_SIZE_T(i)))
		}
		result.Components[c] = plane
	}
	return result, nil
}

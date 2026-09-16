package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func jsonUnmarshalStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("文件包含多个 JSON 值")
	}
	return nil
}

func yamlUnmarshal(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("文件包含多个 YAML 文档")
	}
	return nil
}

// LoadMetadata 从 JSON、YAML 或类 YAML 文件中加载档案描述元数据。
func LoadMetadata(filename string) (Metadata, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".yaml" || ext == ".yml" {
		err = yamlUnmarshal(data, &metadata)
	} else if ext == ".json" {
		err = jsonUnmarshalStrict(data, &metadata)
	} else {
		return Metadata{}, fmt.Errorf("档案元数据文件必须使用 .json、.yaml 或 .yml 扩展名")
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("解析档案元数据失败：%w", err)
	}
	return metadata, nil
}

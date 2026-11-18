package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/beevik/etree"
)

func copyField(name string) string {
	switch name {
	case "certificateCopyCreationTime", "JZJZZSJ":
		return "certificateCopyCreationTime"
	case "certificateCopyCreator", "JZJZZZ":
		return "certificateCopyCreator"
	case "certificateCopyCause", "JZJZZSY":
		return "certificateCopyCause"
	case "certificateCopyExpiringTime", "JZJYXQJZSJ":
		return "certificateCopyExpiringTime"
	default:
		return ""
	}
}

func (m CopyMetadata) validate() error {
	if m.CreationTime.IsZero() || m.CreationTime.Year() < 1 || m.CreationTime.Year() > 9999 || strings.TrimSpace(m.Creator) == "" || strings.TrimSpace(m.Cause) == "" {
		return fmt.Errorf("%w: 加注件制作时间、制作方与事由须非空", ErrOptions)
	}
	date, err := time.Parse("2006-01-02", m.ExpiringDate)
	if err != nil || date.Year() < 1 {
		return fmt.Errorf("%w: 工作流要求加注件截止日期为 YYYY-MM-DD", ErrOptions)
	}
	if m.CreationTime.Format("2006-01-02") > m.ExpiringDate {
		return fmt.Errorf("%w: 加注件截止日期早于制作日期", ErrOptions)
	}
	return nil
}

func (s *snapshot) checkCopyMetadata() error {
	values := map[string]string{}
	for _, entry := range s.metadata.Entries() {
		name := copyField(entry.Name)
		if name == "" {
			continue
		}
		if _, exists := values[name]; exists {
			return fmt.Errorf("%w: 加注件元数据重复", ErrInvalid)
		}
		values[name] = entry.Value
	}
	at, err := time.Parse(time.RFC3339, values["certificateCopyCreationTime"])
	if err != nil {
		return fmt.Errorf("%w: 工作流要求制作时间为 RFC3339", ErrInvalid)
	}
	m := CopyMetadata{CreationTime: at, Creator: values["certificateCopyCreator"],
		Cause: values["certificateCopyCause"], ExpiringDate: values["certificateCopyExpiringTime"]}
	if err := m.validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

func (s *snapshot) setCopyMetadata(m CopyMetadata) {
	for _, node := range children(s.custom, "CustomData") {
		if copyField(node.SelectAttrValue("Name", "")) != "" {
			s.custom.RemoveChild(node)
		}
	}
	for _, field := range [][2]string{
		{"certificateCopyCreationTime", m.CreationTime.Format(time.RFC3339)},
		{"certificateCopyCreator", m.Creator},
		{"certificateCopyCause", m.Cause},
		{"certificateCopyExpiringTime", m.ExpiringDate},
	} {
		node := etree.NewElement("CustomData")
		node.Space = s.custom.Space
		node.CreateAttr("Name", field[0])
		node.SetText(field[1])
		s.custom.AddChild(node)
	}
}

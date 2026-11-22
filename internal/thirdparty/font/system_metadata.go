package font

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
)

// ReadFontMetadata reads names, styles and face indices without decoding glyphs.
// OpenType 1.9.1 Font Collections specifies file-relative table offsets, including
// shared tables and face directories that need not occur in ascending order.
func ReadFontMetadata(r io.ReadSeeker) ([]FontMetadata, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	header, err := read(r, 12)
	if err != nil {
		return nil, err
	}
	if string(header[:4]) != "ttcf" {
		metadata, err := readSFNTMetadataAt(r, 0)
		if err != nil {
			return nil, err
		}
		return []FontMetadata{metadata}, nil
	}
	version, count := u32(header[4:]), u32(header[8:])
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if (version != 0x00010000 && version != 0x00020000) ||
		count == 0 || count > 65536 || int64(count)*4 > size-12 {
		return nil, fmt.Errorf("%w: invalid font collection header", ErrInvalidFontData)
	}
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return nil, err
	}
	offsets, err := read(r, int(count)*4)
	if err != nil {
		return nil, err
	}
	metadatas := make([]FontMetadata, 0, count)
	for i := range int(count) {
		offset := int64(u32(offsets[4*i:]))
		if offset > size-12 {
			continue
		}
		metadata, err := readSFNTMetadataAt(r, offset)
		if err != nil {
			continue // One invalid face must not hide the remaining valid faces.
		}
		metadata.Index = i
		metadatas = append(metadatas, metadata)
	}
	if len(metadatas) == 0 {
		return nil, fmt.Errorf("%w: font collection contains no usable metadata", ErrInvalidFontData)
	}
	return metadatas, nil
}

func readSFNTMetadataAt(r io.ReadSeeker, offset int64) (FontMetadata, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return FontMetadata{}, err
	}
	header, err := read(r, 12)
	if err != nil {
		return FontMetadata{}, err
	}
	signature := string(header[:4])
	if signature != "OTTO" && signature != "true" && u32(header) != 0x00010000 {
		return FontMetadata{}, fmt.Errorf("%w: invalid SFNT signature", ErrInvalidFontData)
	}
	directory, err := read(r, 16*int(u16(header[4:])))
	if err != nil {
		return FontMetadata{}, err
	}
	tables := map[string][]byte{}
	for i := 0; i < len(directory); i += 16 {
		tag := string(directory[i : i+4])
		if tag != "name" && tag != "OS/2" {
			continue
		}
		length := u32(directory[i+12:])
		if length > 1<<20 {
			return FontMetadata{}, fmt.Errorf("%w: %s metadata table too large", ErrInvalidFontData, tag)
		}
		if _, err := r.Seek(int64(u32(directory[i+8:])), io.SeekStart); err != nil {
			return FontMetadata{}, err
		}
		tables[tag], err = read(r, int(length))
		if err != nil {
			return FontMetadata{}, err
		}
	}
	sfnt := &SFNT{Tables: tables}
	if err := sfnt.parseName(); err != nil {
		return FontMetadata{}, err
	}
	preferredName := func(ids ...NameID) string {
		for _, id := range ids {
			records := sfnt.Name.Get(id)
			for _, englishOnly := range []bool{true, false} {
				for _, record := range records {
					english := record.Platform == PlatformWindows && record.Language == 0x0409 ||
						record.Platform == PlatformMacintosh && record.Language == 0
					if englishOnly && !english {
						continue
					}
					if value := strings.TrimSpace(record.String()); value != "" {
						return value
					}
				}
			}
		}
		return ""
	}
	metadata := FontMetadata{Family: preferredName(NamePreferredFamily, NameFontFamily), Style: UnknownStyle}
	if metadata.Family == "" {
		return FontMetadata{}, fmt.Errorf("%w: font family not found", ErrInvalidFontData)
	}
	for _, id := range []NameID{NameFontFamily, NamePreferredFamily, NameFull, NamePostScript} {
		for _, record := range sfnt.Name.Get(id) {
			value := strings.TrimSpace(record.String())
			if value != "" && !slices.Contains(metadata.Aliases, value) {
				metadata.Aliases = append(metadata.Aliases, value)
			}
			if value != "" && (id == NameFull || id == NamePostScript) {
				metadata.FaceNames = append(metadata.FaceNames, value)
			}
		}
	}
	if os2 := tables["OS/2"]; len(os2) >= 64 {
		weight, selection := int(u16(os2[4:])), u16(os2[62:])
		if 100 <= weight && weight <= 900 {
			metadata.Style = ParseStyleCSS(weight, selection&0x0001 != 0)
			if selection&0x0020 != 0 && metadata.Weight() < Bold {
				metadata.Style = metadata.Style&Italic | Bold
			}
		}
	}
	if metadata.Style == UnknownStyle {
		metadata.Style = ParseStyle(preferredName(NamePreferredSubfamily, NameFontSubfamily))
	}
	if metadata.Style == UnknownStyle {
		return FontMetadata{}, fmt.Errorf("%w: unknown font style", ErrInvalidFontData)
	}
	return metadata, nil
}

// MatchFontFace selects a named face from in-memory font data. If no name matches,
// face zero remains the default. Upright faces may supply synthetic italics.
func MatchFontFace(data []byte, name string, style Style) (FontMetadata, error) {
	metadatas, err := ReadFontMetadata(bytes.NewReader(data))
	if err != nil {
		return FontMetadata{}, err
	}
	fonts := &SystemFonts{Fonts: map[string]map[Style]FontMetadata{}}
	for _, metadata := range metadatas {
		fonts.Add(metadata)
	}
	for _, candidate := range []Style{style, style &^ Italic} {
		if metadata, ok := fonts.Match(name, candidate); ok {
			return metadata, nil
		}
	}
	for _, metadata := range metadatas {
		if metadata.Index == 0 {
			return metadata, nil
		}
	}
	return FontMetadata{}, fmt.Errorf("%w: default collection face is unavailable", ErrInvalidFontData)
}

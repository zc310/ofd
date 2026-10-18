package compare

import (
	"context"
	"sort"
)

func compareSnapshots(ctx context.Context, base, target *snapshot, options Options, remaining *int64) (Comparison, error) {
	c := Comparison{Equal: true, PagesEqual: true, Files: []FileChange{}, Pages: []PageChange{}, DirectoriesAdded: []string{}, DirectoriesRemoved: []string{}}
	pairs, err := matchPages(ctx, selectedPages(base.input.Pages, options.Pages), selectedPages(target.input.Pages, options.Pages), options)
	if err != nil {
		return c, err
	}
	c.DirectoriesAdded = difference(target.input.Directories, base.input.Directories)
	c.DirectoriesRemoved = difference(base.input.Directories, target.input.Directories)
	removed, added := []*File{}, []*File{}
	for i := range base.input.Files {
		before := &base.input.Files[i]
		after := target.files[before.Path]
		if after == nil {
			removed = append(removed, before)
			continue
		}
		if before.SHA256 == after.SHA256 {
			continue
		}
		change := FileChange{Status: "modified", Before: before, After: after}
		c.Files = append(c.Files, change)
		c.Summary.FilesModified++
	}
	for i := range target.input.Files {
		f := &target.input.Files[i]
		if base.files[f.Path] == nil {
			added = append(added, f)
		}
	}
	byHash := map[string][]*File{}
	for _, f := range added {
		byHash[f.SHA256] = append(byHash[f.SHA256], f)
	}
	used := map[string]bool{}
	for _, f := range removed {
		matches := byHash[f.SHA256]
		if len(matches) > 0 {
			after := matches[0]
			byHash[f.SHA256] = matches[1:]
			used[after.Path] = true
			c.Files = append(c.Files, FileChange{Status: "moved", Before: f, After: after})
			c.Summary.FilesMoved++
		} else {
			c.Files = append(c.Files, FileChange{Status: "removed", Before: f})
			c.Summary.FilesRemoved++
		}
	}
	for _, f := range added {
		if !used[f.Path] {
			c.Files = append(c.Files, FileChange{Status: "added", After: f})
			c.Summary.FilesAdded++
		}
	}
	sort.Slice(c.Files, func(i, j int) bool { return changePath(c.Files[i]) < changePath(c.Files[j]) })
	for i := range c.Files {
		if err := ctx.Err(); err != nil {
			return c, err
		}
		change := &c.Files[i]
		if err := compareXMLChange(ctx, base, target, change, options); err != nil {
			return c, err
		}
		if change.Before != nil && change.Before.Kind == "image" {
			var err error
			change.BeforePreview, err = resourcePreview(ctx, base, change.Before, options)
			if err != nil {
				return c, err
			}
		}
		if change.After != nil && change.After.Kind == "image" {
			var err error
			change.AfterPreview, err = resourcePreview(ctx, target, change.After, options)
			if err != nil {
				return c, err
			}
		}
		if err := reserveData(options, change); err != nil {
			return c, err
		}
	}
	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return c, err
		}
		p := PageChange{Before: pair.Before, After: pair.After, Status: "unchanged", Match: pair.Info}
		switch {
		case p.Before == 0:
			p.Status = "added"
			c.Summary.PagesAdded++
		case p.After == 0:
			p.Status = "removed"
			c.Summary.PagesRemoved++
		default:
			a, b := base.input.Pages[p.Before-1], target.input.Pages[p.After-1]
			p.Moved = p.Before != p.After
			p.TextChanged = a.Text != b.Text
			p.SizeChanged = a.Width != b.Width || a.Height != b.Height
			if a.ContentSHA256 != b.ContentSHA256 || p.TextChanged || p.SizeChanged {
				p.Status = "modified"
			}
			if p.Moved {
				c.Summary.PagesMoved++
			}
		}
		beforeText, afterText := pageText(base.input, p.Before), pageText(target.input, p.After)
		if beforeText != afterText {
			p.TextChanged = true
			var err error
			p.TextDiff, p.TextDiffCoarse, err = textDifferences(ctx, beforeText, afterText)
			if err != nil {
				return c, err
			}
		}
		if options.Visual {
			var err error
			p.Visual, err = compareVisual(ctx, base, target, p.Before, p.After, options, remaining)
			if err != nil {
				return c, err
			}
			if p.Visual.ChangedPixels > 0 {
				c.Summary.VisualPagesChanged++
				if p.Status == "unchanged" {
					p.Status = "modified"
				}
			}
		}
		if p.Status == "modified" {
			c.Summary.PagesModified++
		}
		if p.Status != "unchanged" || p.Moved {
			c.Equal = false
			c.PagesEqual = false
		}
		if err := reserveData(options, p); err != nil {
			return c, err
		}
		c.Pages = append(c.Pages, p)
	}
	if len(c.Files) > 0 || len(c.DirectoriesAdded) > 0 || len(c.DirectoriesRemoved) > 0 || base.input.Documents != target.input.Documents {
		c.Equal = false
	}
	return c, nil
}

func compareXMLChange(ctx context.Context, base, target *snapshot, change *FileChange, options Options) error {
	// 内容完全相同的移动文件只改变路径，不需要重新分析 XML。
	if change.Status == "moved" {
		return nil
	}
	var before, after map[string]string
	var beforeData, afterData []byte
	var err error
	if change.Before != nil && change.Before.Kind == "xml" {
		before, beforeData, err = base.xmlFields(ctx, change.Before, options)
		if err != nil {
			return err
		}
	}
	if change.After != nil && change.After.Kind == "xml" {
		after, afterData, err = target.xmlFields(ctx, change.After, options)
		if err != nil {
			return err
		}
	}
	if before != nil && after != nil {
		change.XMLChanges, change.XMLTruncated = fieldChanges(before, after, options.MaxChanges)
	}
	change.XMLView, err = buildXMLView(ctx, *change, before, after, beforeData, afterData)
	return err
}

func changePath(c FileChange) string {
	if c.After != nil {
		return c.After.Path
	}
	return c.Before.Path
}

func difference(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range b {
		set[s] = true
	}
	out := []string{}
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}

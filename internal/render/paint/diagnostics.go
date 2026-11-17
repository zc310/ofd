package paint

import "github.com/zc310/ofd/pkg/spec"

// DiagnoseColors 在每次绘制时重放资源问题，不能因颜色缓存命中而丢失报告。
// 正常透明色及未指定颜色不属于异常；循环引用只访问一次。
func (p *Resolver) DiagnoseColors(resources Resources, report func(string, string, spec.StID), colors ...*spec.CTColor) {
	if resources == nil || report == nil {
		return
	}
	seen := make(map[*spec.CTColor]bool)
	spaces := make(map[spec.StID]bool)
	for len(colors) > 0 {
		c := colors[len(colors)-1]
		colors = colors[:len(colors)-1]
		if c == nil || seen[c] {
			continue
		}
		seen[c] = true
		id := c.ColorSpace
		if id == 0 {
			id = resources.DefaultColorSpace()
		}
		if id != 0 && !spaces[spec.StID(id)] {
			spaces[spec.StID(id)] = true
			space := resources.GetColorSpace(spec.StID(id))
			if space == nil {
				report("color_space_missing", "找不到颜色空间", spec.StID(id))
			} else {
				p.colorSpaceResources(space, resources)
				p.colorSpacesMu.Lock()
				cached := p.colorSpaces[space]
				p.colorSpacesMu.Unlock()
				if cached != nil && cached.profileErr != nil {
					report("color_profile_read_failed", cached.profileErr.Error(), spec.StID(id))
				}
			}
		}
		if c.AxialShd != nil {
			for i := range c.AxialShd.Segment {
				colors = append(colors, &c.AxialShd.Segment[i].Color)
			}
		}
		if c.RadialShd != nil {
			for i := range c.RadialShd.Segment {
				colors = append(colors, &c.RadialShd.Segment[i].Color)
			}
		}
		if c.GouraudShd != nil {
			colors = append(colors, c.GouraudShd.BackColor)
			for i := range c.GouraudShd.Point {
				colors = append(colors, &c.GouraudShd.Point[i].Color)
			}
		}
		for _, shd := range []*spec.CTLaGouraudShd{c.LaGourandShd, c.LaGouraudShd} {
			if shd != nil {
				colors = append(colors, shd.BackColor)
				for i := range shd.Point {
					colors = append(colors, &shd.Point[i].Color)
				}
			}
		}
	}
}

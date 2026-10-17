package fontrepair

import (
	"fmt"
	"math"
)

// Type 2 操作数栈在子程序调用间共享。
// 字宽由首个清栈操作符确定（Adobe 5177 §4.1 注4）。
type type2State struct {
	private       privateDict
	global        [][]byte
	stack         []float64
	transient     [32]float64
	width         float64
	widthSeen     bool
	stems, budget int
}

func (v *type2State) run(program []byte, depth int) error {
	if depth > 10 {
		return fmt.Errorf("Type 2: subroutine depth")
	}
	for pos := 0; pos < len(program); {
		v.budget--
		if v.budget < 0 {
			return fmt.Errorf("Type 2: instruction limit")
		}
		op := int(program[pos])
		pos++
		if op == 28 || op >= 32 {
			var value float64
			if op == 255 {
				if pos > len(program)-4 {
					return fmt.Errorf("Type 2: fixed number truncated")
				}
				value = float64(int32(be.Uint32(program[pos:]))) / 65536
				pos += 4
			} else {
				x, n, e := dictNumber(program[pos-1:])
				if e != nil {
					return e
				}
				value = x
				pos += n - 1
			}
			if len(v.stack) >= 48 {
				return fmt.Errorf("Type 2: stack limit")
			}
			v.stack = append(v.stack, value)
			continue
		}
		if op == 10 || op == 29 {
			if len(v.stack) == 0 {
				return fmt.Errorf("Type 2: missing subroutine")
			}
			n := int(v.stack[len(v.stack)-1])
			v.stack = v.stack[:len(v.stack)-1]
			subrs := v.private.subrs
			if op == 29 {
				subrs = v.global
			}
			bias := 107
			if len(subrs) >= 33900 {
				bias = 32768
			} else if len(subrs) >= 1240 {
				bias = 1131
			}
			n += bias
			if n < 0 || n >= len(subrs) {
				return fmt.Errorf("Type 2: subroutine index")
			}
			if e := v.run(subrs[n], depth+1); e != nil {
				return e
			}
			continue
		}
		if op == 11 {
			return nil
		}
		if op == 12 {
			if pos == len(program) {
				return fmt.Errorf("Type 2: escaped operator truncated")
			}
			op = int(program[pos])
			pos++
			if op >= 34 && op <= 37 {
				v.stack = v.stack[:0]
				continue
			}
			if e := v.calculate(op); e != nil {
				return e
			}
			if len(v.stack) > 48 {
				return fmt.Errorf("Type 2: stack limit")
			}
			continue
		}
		if !v.widthSeen {
			explicit := false
			switch op {
			case 1, 3, 18, 23, 19, 20:
				explicit = len(v.stack)%2 == 1
			case 4, 22:
				explicit = len(v.stack) > 1
			case 21:
				explicit = len(v.stack) > 2
			case 14:
				explicit = len(v.stack) == 1 || len(v.stack) == 5
			}
			if explicit {
				v.width = v.private.nominalWidth + v.stack[0]
				v.stack = v.stack[1:]
			}
			v.widthSeen = true
		}
		switch op {
		case 1, 3, 18, 23:
			v.stems += len(v.stack) / 2
		case 19, 20:
			v.stems += len(v.stack) / 2
			maskBytes := (v.stems + 7) / 8
			if maskBytes > len(program)-pos {
				return fmt.Errorf("Type 2: mask truncated")
			}
			// 当前轮廓后端不支持 cntrmask。它与 hintmask 的栈语义和掩码长度
			// 相同（§4.3），替换只影响提示选择，所有轮廓坐标保持不变。
			if op == 20 {
				program[pos-1] = 19
			}
			pos += maskBytes
		case 14:
			return nil
		case 4, 5, 6, 7, 8, 21, 22, 24, 25, 26, 27, 30, 31:
		default:
			return fmt.Errorf("Type 2: unsupported operator %d", op)
		}
		v.stack = v.stack[:0]
	}
	return nil
}

func (v *type2State) calculate(op int) error {
	s := v.stack
	n := len(s)
	require := func(k int) bool { return n >= k }
	var x float64
	switch op {
	case 3, 4, 10, 11, 12, 15, 24:
		if !require(2) {
			break
		}
		a, b := s[n-2], s[n-1]
		switch op {
		case 3:
			if a != 0 && b != 0 {
				x = 1
			}
		case 4:
			if a != 0 || b != 0 {
				x = 1
			}
		case 10:
			x = a + b
		case 11:
			x = a - b
		case 12:
			if b == 0 {
				break
			}
			x = a / b
		case 15:
			if a == b {
				x = 1
			}
		case 24:
			x = a * b
		}
		v.stack = append(s[:n-2], x)
		return nil
	case 5, 9, 14, 26:
		if !require(1) {
			break
		}
		x = s[n-1]
		switch op {
		case 5:
			if x == 0 {
				x = 1
			} else {
				x = 0
			}
		case 9:
			x = math.Abs(x)
		case 14:
			x = -x
		case 26:
			x = math.Sqrt(x)
		}
		s[n-1] = x
		return nil
	case 18:
		if require(1) {
			v.stack = s[:n-1]
			return nil
		}
	case 20:
		if require(2) {
			i := int(s[n-1])
			if i >= 0 && i < 32 {
				v.transient[i] = s[n-2]
				v.stack = s[:n-2]
				return nil
			}
		}
	case 21:
		if require(1) {
			i := int(s[n-1])
			if i >= 0 && i < 32 {
				s[n-1] = v.transient[i]
				return nil
			}
		}
	case 22:
		if require(4) {
			x = s[n-4]
			if s[n-2] > s[n-1] {
				x = s[n-3]
			}
			v.stack = append(s[:n-4], x)
			return nil
		}
	case 23:
		v.stack = append(s, 0.5)
		return nil
	case 27:
		if require(1) && n < 48 {
			v.stack = append(s, s[n-1])
			return nil
		}
	case 28:
		if require(2) {
			s[n-1], s[n-2] = s[n-2], s[n-1]
			return nil
		}
	case 29:
		if require(2) {
			i := int(s[n-1])
			i = min(max(0, i), n-2)
			s[n-1] = s[n-2-i]
			return nil
		}
	case 30:
		if require(2) {
			count, shift := int(s[n-2]), int(s[n-1])
			s = s[:n-2]
			if count > 0 && count <= len(s) {
				shift = ((shift % count) + count) % count
				part := s[len(s)-count:]
				copyPart := append([]float64(nil), part...)
				for i := range part {
					part[(i+shift)%count] = copyPart[i]
				}
				v.stack = s
				return nil
			}
		}
	}
	return fmt.Errorf("Type 2: invalid arithmetic/stack operator %d", op)
}

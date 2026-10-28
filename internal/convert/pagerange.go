package convert

import (
	"sort"
	"strconv"
	"strings"
)

// ParsePageRange turns a selection such as "1-3, 5, 8-" into sorted zero-based
// page indices. An empty or unusable selection means every page.
func ParsePageRange(expr string, pageCount int) []int {
	if pageCount <= 0 {
		return nil
	}
	all := func() []int {
		out := make([]int, pageCount)
		for i := range out {
			out[i] = i
		}
		return out
	}
	if strings.TrimSpace(expr) == "" {
		return all()
	}
	set := map[int]bool{}
	for _, raw := range strings.Split(expr, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var start, end int
		if dash := strings.IndexByte(raw, '-'); dash < 0 {
			n, err := strconv.Atoi(raw)
			if err != nil {
				continue
			}
			start, end = n, n
		} else {
			left, right := strings.TrimSpace(raw[:dash]), strings.TrimSpace(raw[dash+1:])
			start, end = 1, pageCount
			var err error
			if left != "" {
				if start, err = strconv.Atoi(left); err != nil || start < 0 {
					continue
				}
			}
			if right != "" {
				if end, err = strconv.Atoi(right); err != nil || end < 0 {
					continue
				}
			}
		}
		if start > end {
			start, end = end, start
		}
		for p := max(1, start); p <= min(pageCount, end); p++ {
			set[p-1] = true
		}
	}
	if len(set) == 0 {
		return all()
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

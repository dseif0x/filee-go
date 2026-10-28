package spreadsheet

import (
	"math"

	"github.com/dseif0x/filee-go/internal/xdom"
)

type nodeT = xdom.Node

func mathFloat64bits(v float64) uint64 { return math.Float64bits(v) }

func parseFragment(x string) (*xdom.Node, error) {
	return xdom.ParseBytes([]byte(`<r xmlns:number="` + numberNs + `" xmlns:style="` + styleNs + `" xmlns:fo="` + foNs + `">` + x + `</r>`))
}

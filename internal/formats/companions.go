package formats

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Shapefile is a .shp with its .shx, .dbf, .prj, … of the same name. They
// travel with the .shp and, dropped together with it, are part of it rather
// than files of their own.
var shapefileParts = []string{"shx", "dbf", "prj", "cpg", "sbn", "sbx", "fbn", "fbx", "ain", "aih", "atx", "ixs", "mxs", "qix", "qmd", "qpj", "shp.xml"}

// Companions returns the companion files next to a .shp (any letter case), in part order.
func Companions(path string) []string {
	if !isLocal(path) || !isShapefile(path) {
		return nil
	}
	full, _ := filepath.Abs(path)
	dir, file := filepath.Split(full)
	name := strings.TrimSuffix(file, filepath.Ext(file))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type part struct {
		path string
		idx  int
	}
	var found []part
	for _, e := range entries {
		if i := partOf(e.Name(), name); i >= 0 && !e.IsDir() {
			found = append(found, part{filepath.Join(dir, e.Name()), i})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].idx < found[j].idx })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}

// WithoutCompanions drops the companion files of shapefiles that are in paths too.
func WithoutCompanions(paths []string) []string {
	shp := map[string]bool{}
	for _, p := range paths {
		if isLocal(p) && isShapefile(p) {
			full, _ := filepath.Abs(p)
			shp[strings.ToLower(strings.TrimSuffix(full, filepath.Ext(full)))] = true
		}
	}
	if len(shp) == 0 {
		return paths
	}
	var out []string
	for _, p := range paths {
		if !companionOf(p, shp) {
			out = append(out, p)
		}
	}
	return out
}

func companionOf(path string, shp map[string]bool) bool {
	if !isLocal(path) || isShapefile(path) {
		return false
	}
	full, _ := filepath.Abs(path)
	lower := strings.ToLower(full)
	for _, part := range shapefileParts {
		if strings.HasSuffix(lower, "."+part) && len(filepath.Base(full)) > len(part)+1 && shp[lower[:len(lower)-len(part)-1]] {
			return true
		}
	}
	return false
}

func partOf(file, name string) int {
	if len(file) <= len(name)+1 || !strings.EqualFold(file[:len(name)+1], name+".") {
		return -1
	}
	rest := file[len(name)+1:]
	for i, p := range shapefileParts {
		if strings.EqualFold(p, rest) {
			return i
		}
	}
	return -1
}

func isShapefile(p string) bool { return strings.EqualFold(filepath.Ext(p), ".shp") }

func isLocal(p string) bool { return !strings.Contains(p, "://") && filepath.IsAbs(p) }

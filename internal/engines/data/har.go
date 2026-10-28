package data

import "errors"

// HAR (HTTP Archive): the network requests a browser's developer tools record,
// saved as JSON. Read like JSON for the tree targets; as a table, one row per
// request with what a reader looks for first. Pages, timings and headers stay
// in the JSON.

var harColumns = []string{"Started", "Method", "URL", "Status", "Time (ms)", "Size (bytes)", "Type"}

// harTable is the requests of a HAR log (log.entries) as rows under a header row.
func harTable(tree any) ([][]string, error) {
	log, ok := get(tree, "log").(*Map)
	if !ok {
		return nil, errors.New("The file isn't a HAR log: it has no log.entries list.")
	}
	entries, ok := get(log, "entries").([]any)
	if !ok {
		return nil, errors.New("The file isn't a HAR log: it has no log.entries list.")
	}
	rows := [][]string{append([]string{}, harColumns...)}
	for _, entry := range entries {
		request := get(entry, "request")
		response := get(entry, "response")
		content := get(response, "content")
		// The body's size is in content.size; bodySize is -1 when the browser didn't know it.
		size := get(content, "size")
		if size == nil {
			size = get(response, "bodySize")
		}
		sizeText := harText(size)
		if n, ok := size.(int64); ok && n < 0 {
			sizeText = ""
		}
		rows = append(rows, []string{
			harText(get(entry, "startedDateTime")),
			harText(get(request, "method")),
			harText(get(request, "url")),
			harText(get(response, "status")),
			harText(get(entry, "time")),
			sizeText,
			harText(get(content, "mimeType")),
		})
	}
	return rows, nil
}

func get(node any, key string) any {
	if m, ok := node.(*Map); ok {
		v, _ := m.Get(key)
		return v
	}
	return nil
}

func harText(v any) string {
	if isScalar(v) {
		return text(v)
	}
	return ""
}

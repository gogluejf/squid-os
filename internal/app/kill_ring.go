package app

// diffKilled returns the text removed from `before` to produce `after`, when
// the change is a pure deletion of a single contiguous substring (which is
// exactly what readline kill bindings do: ctrl+k, ctrl+u, ctrl+w, alt+d).
// Returns "" if nothing was deleted or the change involved insertions.
func diffKilled(before, after string) string {
	if len(after) >= len(before) {
		return ""
	}
	// Shared prefix length.
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	// Shared suffix length (not overlapping the prefix).
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	return before[prefix : len(before)-suffix]
}

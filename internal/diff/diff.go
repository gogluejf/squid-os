package diff

import (
	"fmt"
	"strings"
)

// Diff returns a unified diff string between oldContent and newContent.
// Only changed lines with +/- prefixes are included, plus minimal context.
// Returns "" if content is identical.
func Diff(oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	var oldLines, newLines []string
	if oldContent != "" {
		oldLines = strings.Split(oldContent, "\n")
	}
	if newContent != "" {
		newLines = strings.Split(newContent, "\n")
	}
	// Strip trailing empty element from Split when content ends with "\n"
	// (Go gotcha: "a\nb\n".Split("\n") → ["a", "b", ""] → index shift)
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" && strings.HasSuffix(oldContent, "\n") {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" && strings.HasSuffix(newContent, "\n") {
		newLines = newLines[:len(newLines)-1]
	}
	return formatDiff(oldLines, newLines)
}

func formatDiff(oldLines, newLines []string) string {
	matrix := lcsMatrix(oldLines, newLines)
	ops := backtrack(matrix, oldLines, newLines)
	return formatOps(ops, oldLines, newLines)
}

func lcsMatrix(a, b []string) [][]int {
	m, n := len(a), len(b)
	matrix := make([][]int, m+1)
	for i := 0; i <= m; i++ {
		matrix[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				matrix[i][j] = matrix[i-1][j-1] + 1
			} else if matrix[i-1][j] > matrix[i][j-1] {
				matrix[i][j] = matrix[i-1][j]
			} else {
				matrix[i][j] = matrix[i][j-1]
			}
		}
	}
	return matrix
}

type opType int

const (
	opEqual opType = iota
	opRemove
	opAdd
)

type editOp struct {
	typ opType
	i, j int
}

func backtrack(matrix [][]int, a, b []string) []editOp {
	var ops []editOp
	i, j := len(a), len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, editOp{opEqual, i - 1, j - 1})
			i--
			j--
		} else if j > 0 && (i == 0 || matrix[i][j-1] >= matrix[i-1][j]) {
			ops = append(ops, editOp{opAdd, i, j - 1})
			j--
		} else {
			ops = append(ops, editOp{opRemove, i - 1, j})
			i--
		}
	}
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

func formatOps(ops []editOp, oldLines, newLines []string) string {
	type segment struct {
		typ  opType
		text string
		old  int // index in oldLines (for removes and context)
		new  int // index in newLines (for adds and context)
	}

	// First pass: find change indices (non-equal ops)
	var changeIndices []int
	for i, op := range ops {
		if op.typ != opEqual {
			changeIndices = append(changeIndices, i)
		}
	}
	if len(changeIndices) == 0 {
		return ""
	}

	// Second pass: for each change, gather it plus 1 line of context before/after
	seen := make(map[int]bool)
	var output []segment
	for _, ci := range changeIndices {
		// Context line before (if not already output)
		if ci > 0 && ops[ci-1].typ == opEqual && !seen[ci-1] {
			seen[ci-1] = true
			op := ops[ci-1]
			output = append(output, segment{opEqual, oldLines[op.i], op.i, op.j})
		}
		// The change itself
		seen[ci] = true
		op := ops[ci]
		switch op.typ {
		case opRemove:
			output = append(output, segment{opRemove, oldLines[op.i], op.i, -1})
		case opAdd:
			output = append(output, segment{opAdd, newLines[op.j], -1, op.j})
		}
		// Context line after (if not already output)
		if ci < len(ops)-1 && ops[ci+1].typ == opEqual && !seen[ci+1] {
			seen[ci+1] = true
			op := ops[ci+1]
			output = append(output, segment{opEqual, oldLines[op.i], op.i, op.j})
		}
	}

	var b strings.Builder
	for _, s := range output {
		switch s.typ {
		case opEqual:
			// Context: embed both old and new line numbers (1-based) as "OLD|NEW|text"
			b.WriteString(fmt.Sprintf("    %d|%d|%s\n", s.old+1, s.new+1, s.text))
		case opRemove:
			// Remove: embed old line number (1-based) as "OLD|-|text"
			b.WriteString(fmt.Sprintf("  - %d|-|%s\n", s.old+1, s.text))
		case opAdd:
			// Add: embed new line number (1-based) as "-|NEW|text"
			b.WriteString(fmt.Sprintf("  + -|%d|%s\n", s.new+1, s.text))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

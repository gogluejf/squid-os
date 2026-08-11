package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"

	"squid-os/internal/util"
)

func parseIntegralArg(args map[string]interface{}, name string) (int, bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, false, nil
	}
	switch n := v.(type) {
	case int:
		return n, true, nil
	case int8:
		return int(n), true, nil
	case int16:
		return int(n), true, nil
	case int32:
		return int(n), true, nil
	case int64:
		if int64(int(n)) != n {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(n), true, nil
	case uint:
		if uint(int(n)) != n {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(n), true, nil
	case uint8:
		return int(n), true, nil
	case uint16:
		return int(n), true, nil
	case uint32:
		if uint32(int(n)) != n {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(n), true, nil
	case uint64:
		if uint64(int(n)) != n {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(n), true, nil
	case float64:
		if math.Trunc(n) != n {
			return 0, false, fmt.Errorf("%s must be an integer, got %v", name, n)
		}
		maxInt := int(^uint(0) >> 1)
		minInt := -maxInt - 1
		if n < float64(minInt) || n > float64(maxInt) {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(n), true, nil
	case float32:
		f := float64(n)
		if math.Trunc(f) != f {
			return 0, false, fmt.Errorf("%s must be an integer, got %v", name, n)
		}
		return int(n), true, nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false, fmt.Errorf("%s must be an integer, got %q", name, n.String())
		}
		if int64(int(i)) != i {
			return 0, false, fmt.Errorf("%s is out of range for int", name)
		}
		return int(i), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be numeric integer, got %T", name, v)
	}
}

// ResolvePath resolves a path against the provided session working directory.
func ResolvePath(p, workingDir string) string {
	p = util.ExpandHome(p)
	if filepath.IsAbs(p) {
		return p
	}
	if workingDir != "" {
		return filepath.Join(workingDir, p)
	}
	return p
}

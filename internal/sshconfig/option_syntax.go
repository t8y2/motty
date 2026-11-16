package sshconfig

import (
	"fmt"
	"strconv"
	"strings"
)

func parseNonnegativeOption(value string, duration bool) (int, error) {
	invalid := fmt.Errorf("invalid non-negative value %q", value)
	if value == "" {
		return 0, invalid
	}
	var total int64
	for value != "" {
		end := 0
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		if end == 0 {
			return 0, invalid
		}
		number, err := strconv.ParseInt(value[:end], 10, 32)
		if err != nil {
			return 0, invalid
		}
		value = value[end:]
		multiplier := int64(1)
		if value != "" {
			if !duration {
				return 0, invalid
			}
			switch strings.ToLower(value[:1]) {
			case "s":
			case "m":
				multiplier = 60
			case "h":
				multiplier = 3600
			case "d":
				multiplier = 86400
			case "w":
				multiplier = 604800
			default:
				return 0, invalid
			}
			value = value[1:]
		}
		total += number * multiplier
		if total > 2147483647 {
			return 0, invalid
		}
	}
	return int(total), nil
}

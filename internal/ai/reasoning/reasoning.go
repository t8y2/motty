package reasoning

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"motty/internal/ai/store"
)

// 标准化思考档位枚举（对齐行业标准）
const (
	LevelDefault = "default" // 默认（使用模型自身默认设定）
	LevelNone    = "none"    // 明确关闭思考 / 0 预算
	LevelMinimal = "minimal" // 极简（约 1024 tokens）
	LevelLow     = "low"     // 低
	LevelMedium  = "medium"  // 中
	LevelHigh    = "high"    // 高
	LevelXHigh   = "xhigh"   // 极高（约 16k tokens）
	LevelMax     = "max"     // 最大可用预算
)

// AllLevels 包含所有受支持的标准思考档位
var AllLevels = []string{
	LevelDefault,
	LevelNone,
	LevelMinimal,
	LevelLow,
	LevelMedium,
	LevelHigh,
	LevelXHigh,
	LevelMax,
}

// levelScale 将思考档位映射为有序标量，用于就近距离度量与降级
var levelScale = map[string]int{
	LevelNone:    0,
	LevelMinimal: 1,
	LevelLow:     2,
	LevelMedium:  3,
	LevelHigh:    4,
	LevelXHigh:   5,
	LevelMax:     6,
}

// IsValidLevel 检查给定的档位字符串是否是标准档位之一
func IsValidLevel(level string) bool {
	return slices.Contains(AllLevels, level)
}

// NearestLevel 在模型实际支持的 supportedLevels 中寻找最匹配 requestedLevel 的档位。
// 规则：
// 1. 若 supported 为空，返回 ""
// 2. 若直接命中，返回原值
// 3. 若 requested == "default"，优先返回 supported 包含的 "default"，否则返回第一个非 "none" 档位
// 4. 若无法直接命中，按有序标量距离就近匹配
func NearestLevel(requested string, supported []string) string {
	if len(supported) == 0 {
		return ""
	}
	if slices.Contains(supported, requested) {
		return requested
	}
	if requested == LevelDefault {
		for _, s := range supported {
			if s != LevelNone {
				return s
			}
		}
		return supported[0]
	}

	reqScore, hasReq := levelScale[requested]
	if !hasReq {
		return supported[0]
	}

	best := supported[0]
	minDist := math.MaxInt32
	for _, s := range supported {
		score, hasScore := levelScale[s]
		if !hasScore {
			continue
		}
		dist := int(math.Abs(float64(reqScore - score)))
		if dist < minDist {
			minDist = dist
			best = s
		}
	}
	return best
}

// FromModel 从 store.Model 读取并校验已保存的 ReasoningConfig
func FromModel(m store.Model) (*store.ReasoningConfig, error) {
	if m.Reasoning == nil {
		return nil, nil
	}
	for _, l := range m.Reasoning.Levels {
		if !IsValidLevel(l) {
			return nil, fmt.Errorf("invalid reasoning level %q: %w", l, errors.New("unsupported level"))
		}
	}
	return m.Reasoning, nil
}

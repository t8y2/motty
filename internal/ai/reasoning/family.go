package reasoning

import (
	"regexp"

	"motty/internal/ai/store"
)

var (
	// 否定守卫：明确标记为不思考、关闭思考或非推理版
	negativeGuardRegex = regexp.MustCompile(`(?i)(?:-non-reasoning|-no-think|-nothinking)\b`)

	// 显式通用语义线索：只要包含推理/思考相关词根，均判定为思考模型
	semanticClueRegex = regexp.MustCompile(`(?i)\b(?:reasoning|reasoner|thinking|thought|cot)\b|-(?:thinking|think)$`)

	// 1. OpenAI o-系列代际泛化：o1, o3, o4, o5... 及其 -mini, -preview 变体
	openAIORegex = regexp.MustCompile(`(?i)(?:^|[-_/])o[1-9](?:-mini|-preview)?(?:$|[-_/])`)

	// 2. DeepSeek 系列代际泛化：-r1, -r2, -r3... 或 deepseek-v4, deepseek-v5... 或 deepseek-flash
	deepSeekGenRegex = regexp.MustCompile(`(?i)(?:-(?:r\d+|reasoner)\b|^deepseek-(?:v[4-9]|flash))`)

	// 3. GLM 智谱代际泛化：glm-5, glm-6, glm-7... 及其 flash/fast 变体
	glmGenRegex = regexp.MustCompile(`(?i)^glm-(?:[5-9]|\d{2,})`)

	// 4. Google Gemini 代际泛化：gemini-3, gemini-4... 及其 flash, pro 变体
	geminiGenRegex = regexp.MustCompile(`(?i)^gemini-(?:[3-9]|\d{2,})`)

	// 5. Claude 代际泛化：claude-3.7, claude-3-7, claude-4... 思考代际
	claudeGenRegex = regexp.MustCompile(`(?i)^claude-(?:3[.-][7-9]|[4-9][.-])`)

	// 6. Qwen 阿里系列：qwq, qvq 等思考型号
	qwenGenRegex = regexp.MustCompile(`(?i)^(?:qwq|qvq)\b`)

	// 预分配各厂商标准阶梯配置，避免重复堆分配
	cfgOpenAIO = &store.ReasoningConfig{
		Levels:  []string{LevelLow, LevelMedium, LevelHigh},
		Default: LevelMedium,
	}
	cfgDeepSeek = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelDefault, LevelHigh},
		Default: LevelDefault,
	}
	cfgGLM = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelLow, LevelMedium, LevelHigh},
		Default: LevelMedium,
	}
	cfgGemini = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelMinimal, LevelLow, LevelMedium, LevelHigh},
		Default: LevelMedium,
	}
	// Claude thinking costs output tokens, so it is opt-in: the default is off.
	cfgClaude = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelLow, LevelMedium, LevelHigh, LevelXHigh, LevelMax},
		Default: LevelNone,
	}
	cfgQwen = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelDefault, LevelHigh},
		Default: LevelDefault,
	}
	cfgSemantic = &store.ReasoningConfig{
		Levels:  []string{LevelNone, LevelLow, LevelMedium, LevelHigh},
		Default: LevelMedium,
	}
)

// InferReasoning 根据模型名称（支持各种复杂中转路径、tag 与修饰）
// 深度归一化并结合通用形态学与代际启发式规则推断其思考能力与推荐档位。
// 若判定不是推理模型，返回 nil。
func InferReasoning(rawName string) *store.ReasoningConfig {
	base := CanonicalBaseName(rawName)
	if base == "" {
		return nil
	}

	// 第一步：否定守卫 (Negative Guard)
	if negativeGuardRegex.MatchString(base) || negativeGuardRegex.MatchString(rawName) {
		return nil
	}

	// 第二步：各大主流厂商代际模式泛化匹配（提供专属精准档位）
	if openAIORegex.MatchString(base) {
		return cfgOpenAIO
	}

	if deepSeekGenRegex.MatchString(base) {
		return cfgDeepSeek
	}

	if glmGenRegex.MatchString(base) {
		return cfgGLM
	}

	if geminiGenRegex.MatchString(base) {
		return cfgGemini
	}

	if claudeGenRegex.MatchString(base) {
		return cfgClaude
	}

	if qwenGenRegex.MatchString(base) {
		return cfgQwen
	}

	// 第三步：通用语义线索兜底（支持各类开源自建、私有微调或新厂商未知模型）
	if semanticClueRegex.MatchString(base) {
		return cfgSemantic
	}

	return nil
}

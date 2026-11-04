package workbench

import (
	"fmt"
	"time"

	"motty/internal/ai/llm"
)

type aiRunPhase uint8

const (
	aiRunWaitingModel aiRunPhase = iota
	aiRunAnalyzing
	aiRunAnswering
	aiRunUsingTool
	aiRunStopping
	aiRunCompleted
	aiRunStopped
	aiRunFailed
	aiRunNeedsApproval
)

type aiRunStats struct {
	startedAt       time.Duration
	duration        time.Duration
	phase           aiRunPhase
	tool            string
	usage           llm.Usage
	usageIncomplete bool
}

func (state *State) aiRunStatusText() (status, usage string) {
	ai, messages := &state.ai, state.messages.AIPanel
	stats := ai.lastRun
	running := ai.run != nil
	if running {
		stats = &ai.run.stats
	}
	if stats == nil {
		return "", ""
	}
	duration := stats.duration
	phase := stats.phase
	if running {
		duration = max(state.now()-stats.startedAt, 0)
		if phase != aiRunStopping && len(ai.approvals) > 0 {
			phase = aiRunNeedsApproval
		}
	} else if phase == aiRunNeedsApproval {
		phase = aiRunCompleted
		for _, approval := range ai.approvals {
			if approval.escalation {
				phase = aiRunNeedsApproval
				break
			}
		}
	}
	label := messages.RunWaiting
	switch phase {
	case aiRunWaitingModel:
		if duration >= 15*time.Second {
			label = messages.RunWaitingLong
		}
	case aiRunAnalyzing:
		label = messages.ThinkingNow
	case aiRunAnswering:
		label = messages.RunAnswering
	case aiRunUsingTool:
		label = fmt.Sprintf(messages.RunToolFmt, aiToolTitle(messages, stats.tool))
	case aiRunStopping:
		label = messages.RunStopping
	case aiRunCompleted:
		label = messages.RunCompleted
	case aiRunStopped:
		label = messages.RunStopped
	case aiRunFailed:
		label = messages.StatusFailed
	case aiRunNeedsApproval:
		label = messages.RunApproval
	}
	durationFormat := messages.RunTotalTimeFmt
	if running {
		durationFormat = messages.RunElapsedFmt
	}
	status = label + " · " + fmt.Sprintf(durationFormat, aiRunDurationText(duration))
	if running {
		return status, ""
	}
	usage = messages.RunUsageUnknown
	if stats.usage != (llm.Usage{}) {
		usage = fmt.Sprintf(messages.RunUsageFmt, groupDigits(stats.usage.InputTokens+stats.usage.OutputTokens), groupDigits(stats.usage.InputTokens), groupDigits(stats.usage.OutputTokens))
		if stats.usage.CacheReadTokens > 0 {
			usage += fmt.Sprintf(messages.UsageCachedFmt, groupDigits(stats.usage.CacheReadTokens))
		}
		if stats.usageIncomplete {
			usage += messages.RunUsageIncomplete
		}
	}
	total := ai.totalUsage
	if total.InputTokens > stats.usage.InputTokens || total.OutputTokens > stats.usage.OutputTokens {
		usage += "\n" + fmt.Sprintf(messages.RunUsageConversationFmt, groupDigits(total.InputTokens+total.OutputTokens))
	}
	return status, usage
}

func aiRunDurationText(duration time.Duration) string {
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", max(duration, 0).Seconds())
	}
	seconds := int(duration / time.Second)
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}

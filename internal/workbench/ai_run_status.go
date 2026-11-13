package workbench

import (
	"fmt"
	"strconv"
	"time"

	"motty/internal/ai/llm"
	"motty/internal/i18n"
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
	// provider and model are the request that spent the usage, as the card
	// names them.
	provider string
	model    string
	// restored marks the accounting of a saved conversation: it has a cost but
	// no start, so it reports no time.
	restored bool
}

// aiUsage is one finished run as the footer shows it: what it spent, how much
// of its prompt came from the provider's cache, and the request that spent it.
type aiUsage struct {
	usage      llm.Usage
	total      llm.Usage // the conversation's total, this run included
	provider   string
	model      string
	incomplete bool
}

// known says whether the provider reported any usage for the run.
func (u aiUsage) known() bool { return u.usage != (llm.Usage{}) }

// spent is what the provider billed: every prompt token, cached or not, and
// the answer.
func (u aiUsage) spent() int { return u.usage.InputTokens + u.usage.OutputTokens }

// uncached is the part of the prompt the provider had to read from its own
// state rather than from cache.
func (u aiUsage) uncached() int { return max(u.usage.InputTokens-u.usage.CacheReadTokens, 0) }

// hitRate is the share of the prompt served from cache, to one decimal place,
// and whether there was a prompt to measure.
func (u aiUsage) hitRate() (string, bool) {
	if u.usage.InputTokens <= 0 {
		return "", false
	}
	permille := (u.usage.CacheReadTokens*1000 + u.usage.InputTokens/2) / u.usage.InputTokens
	if permille%10 == 0 {
		return fmt.Sprintf("%d%%", permille/10), true
	}
	return fmt.Sprintf("%d.%d%%", permille/10, permille%10), true
}

// modelLine names the request's provider and model, and whichever of the two
// the run knows.
func (u aiUsage) modelLine() string {
	if u.provider != "" && u.model != "" {
		return u.provider + "/" + u.model
	}
	return u.provider + u.model
}

// chipLabel is the footer chip: the run's total in compact form.
func (u aiUsage) chipLabel(messages i18n.AIPanelMessages) string {
	if !u.known() {
		return messages.UsageChipUnknown
	}
	return fmt.Sprintf(messages.UsageChipFmt, aiCompactTokens(u.spent()))
}

// usageCardRow is one line of the card: a label and its value.
type usageCardRow struct {
	label string
	value string
}

// rows are the card's numbers, in the order the card lists them. A prompt the
// provider served entirely from its own state shows no cache rows, and the
// conversation total appears only once it exceeds this run.
func (u aiUsage) rows(messages i18n.AIPanelMessages) []usageCardRow {
	var rows []usageCardRow
	if line := u.modelLine(); line != "" {
		rows = append(rows, usageCardRow{messages.UsageCardProvider, line})
	}
	if !u.known() {
		return rows
	}
	if u.usage.CacheReadTokens > 0 {
		if rate, ok := u.hitRate(); ok {
			rows = append(rows, usageCardRow{messages.UsageCardHitRate, rate})
		}
		rows = append(rows, usageCardRow{messages.UsageCardUncached, u.tokens(messages, u.uncached())})
		rows = append(rows, usageCardRow{messages.UsageCardCached, u.tokens(messages, u.usage.CacheReadTokens)})
	}
	rows = append(rows, usageCardRow{messages.UsageCardOutput, u.tokens(messages, u.usage.OutputTokens)})
	if u.total.InputTokens > u.usage.InputTokens || u.total.OutputTokens > u.usage.OutputTokens {
		rows = append(rows, usageCardRow{messages.UsageCardTotal, u.tokens(messages, u.total.InputTokens+u.total.OutputTokens)})
	}
	return rows
}

// note is the card's remark under its rows, when the numbers are not all it
// should say.
func (u aiUsage) note(messages i18n.AIPanelMessages) string {
	switch {
	case !u.known():
		return messages.UsageCardNone
	case u.incomplete:
		return messages.UsageCardIncomplete
	default:
		return ""
	}
}

func (u aiUsage) tokens(messages i18n.AIPanelMessages, n int) string {
	return fmt.Sprintf(messages.UsageCardTokensFmt, groupDigits(n))
}

// aiCompactTokens writes a token count the way the chip does: exact below a
// thousand, then one decimal place of K, M or B.
func aiCompactTokens(n int) string {
	switch {
	case n < 1_000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return compactTenths(n, 1_000) + "K"
	case n < 1_000_000_000:
		return compactTenths(n, 1_000_000) + "M"
	default:
		return compactTenths(n, 1_000_000_000) + "B"
	}
}

// compactTenths rounds n/unit to one decimal place and drops a trailing zero,
// so 13,276,665 becomes "13.3" and 153,400 becomes "153.4".
func compactTenths(n, unit int) string {
	tenths := (n*10 + unit/2) / unit
	if tenths%10 == 0 {
		return strconv.Itoa(tenths / 10)
	}
	return fmt.Sprintf("%d.%d", tenths/10, tenths%10)
}

func (state *State) aiRunStatusText() (status, usage string) {
	ai, messages := &state.ai, state.messages.AIPanel
	stats := ai.lastRun
	active := state.aiRunActive()
	if ai.run != nil {
		stats = &ai.run.stats
	} else if ai.pendingRun != nil {
		stats = ai.pendingRun
	}
	if stats == nil {
		return "", ""
	}
	duration := stats.duration
	phase := stats.phase
	if active {
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
	if active {
		durationFormat = messages.RunElapsedFmt
	}
	status = label + " · " + fmt.Sprintf(durationFormat, aiRunDurationText(duration))
	if stats.restored {
		// A restored run knows what it cost but not when it ran.
		status = ""
	}
	if active {
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

func (state *State) aiRunActive() bool {
	return state.ai.run != nil || state.ai.pendingRun != nil
}

func (state *State) aiRecordFinishedRun(stats *aiRunStats) {
	ai := &state.ai
	ai.lastUsage = stats.usage
	ai.totalUsage = ai.totalUsage.Add(stats.usage)
	ai.lastRun = stats
}

// aiRunUsage is the run the footer's chip prices, and whether there is one: a
// request still in flight or waiting for an approval has nothing settled to
// show, and a restored conversation shows the request that wrote its last
// answer.
func (state *State) aiRunUsage() (aiUsage, bool) {
	ai := &state.ai
	stats := ai.lastRun
	if stats == nil || ai.run != nil || ai.pendingRun != nil {
		return aiUsage{}, false
	}
	return aiUsage{
		usage:      stats.usage,
		total:      ai.totalUsage,
		provider:   stats.provider,
		model:      stats.model,
		incomplete: stats.usageIncomplete,
	}, true
}

func (state *State) aiFinishPendingRun(phase aiRunPhase) {
	stats := state.ai.pendingRun
	if stats == nil {
		return
	}
	state.ai.pendingRun = nil
	stats.duration = max(state.now()-stats.startedAt, 0)
	stats.phase = phase
	state.aiRecordFinishedRun(stats)
	state.aiPricePendingSpend(stats)
}

func aiRunDurationText(duration time.Duration) string {
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", max(duration, 0).Seconds())
	}
	seconds := int(duration / time.Second)
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}

package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// DeepSeek describes tool calls with a textual markup protocol, DSML, that a
// serving lane can leak into the message content instead of returning
// structured tool_calls. A leaked block is shown to the user as an answer,
// runs nothing, and ends the turn as if the model had finished; the leaked
// text also joins the history, where it teaches the model to leak again.
//
// dsmlFilter repairs that at the wire. It holds back the few bytes that could
// still grow into a tag, turns complete invokes into ToolCalls, and keeps the
// markup out of the text channel. Text without markup passes through without
// copying.
//
// Accepted grammar, with bars being "｜" (U+FF5C) or "|", one or more of them,
// and spaces tolerated between them:
//
//	<｜DSML｜ calls>
//	  <｜DSML｜ invoke name="terminal_exec">
//	    <｜DSML｜ parameter name="command" string="true">uptime</｜DSML｜ parameter>
//	    { "or a bare JSON body": true }
//	  </｜DSML｜ invoke>
//	</｜DSML｜ calls>
//
// A corrupted structural token can lose or misspell a parameter's closer. Its
// value then ends at the next parameter opener or the invoke's end, and any
// DSML closer in between still terminates it, so one damaged tag never swallows
// the parameters that follow.
const (
	// dsmlHoldLimit is how long a candidate tag may be withheld; a real
	// opener is a few dozen bytes, so a longer run is prose that merely
	// started with "<".
	dsmlHoldLimit = 1 << 10
	// dsmlRegionLimit bounds the markup buffered between an opener and its
	// closer. Past this it is a runaway, not a tool call.
	dsmlRegionLimit = 1 << 20
)

var (
	// errDSMLUnrecovered ends a stream whose markup yielded no call: the turn
	// must not count as answered.
	errDSMLUnrecovered = errors.New("llm: the provider wrote a tool call as text and it could not be recovered")
	errDSMLRunaway     = errors.New("llm: the provider wrote an unreasonably long tool-call block as text")
)

type dsmlKind uint8

const (
	dsmlNone dsmlKind = iota
	dsmlBlock
	dsmlInvoke
)

// dsmlFilter is the streaming half of the recovery: it is fed one content
// delta at a time and returns the text that is safe to publish.
//
// Stripping and recovery are separate policies. Markup is protocol, never
// prose, so every streaming channel strips it, including the turns that offer
// no tools. Recovery turns a complete block into tool calls, and only runs when
// the request declared tools: a call fabricated in a turn that cannot run it
// would either be dropped or fail the turn, and a model quoting the markup in
// its reasoning must not start a command.
type dsmlFilter struct {
	strip   bool
	recover bool
	// held is text withheld because it may still grow into a tag.
	held string
	// region is the markup buffered since an opener, and scan is how much of
	// it was already searched for the closer.
	region []byte
	kind   dsmlKind
	scan   int
	// blocks counts the markup regions seen and calls the tool calls
	// recovered from them.
	blocks int
	calls  int
}

func newDSMLFilter(strip, recover bool) dsmlFilter {
	return dsmlFilter{strip: strip, recover: recover}
}

// reset discards an unfinished region. A channel that only strips markup uses
// it to drop a runaway block without ending a turn that can still answer.
func (f *dsmlFilter) reset() {
	f.held = ""
	f.region = f.region[:0]
	f.kind = dsmlNone
	f.scan = 0
}

// push consumes one content delta and returns the text to publish plus any
// tool calls the delta completed.
func (f *dsmlFilter) push(delta string) (string, []ToolCall, error) {
	if !f.strip || delta == "" {
		return delta, nil, nil
	}
	work := delta
	if f.held != "" {
		work = f.held + delta
		f.held = ""
	}
	if f.kind == dsmlNone && strings.IndexByte(work, '<') < 0 {
		return work, nil, nil
	}
	var (
		b     strings.Builder
		calls []ToolCall
	)
	for work != "" {
		if f.kind != dsmlNone {
			f.region = append(f.region, work...)
			work = ""
			if len(f.region) > dsmlRegionLimit {
				return "", nil, errDSMLRunaway
			}
			end := f.findCloser()
			if end < 0 {
				break
			}
			rest := string(f.region[end:])
			calls = append(calls, f.closeRegion(end)...)
			work = rest
			continue
		}
		cut := strings.IndexByte(work, '<')
		if cut < 0 {
			b.WriteString(work)
			break
		}
		b.WriteString(work[:cut])
		tail := work[cut:]
		n, partial := dsmlTag(tail)
		if partial && len(tail) <= dsmlHoldLimit {
			f.held = tail
			break
		}
		if n == 0 || partial {
			// Not markup after all: the "<" is prose.
			b.WriteByte('<')
			work = tail[1:]
			continue
		}
		tag := tail[:n]
		work = tail[n:]
		kind, closer, _ := dsmlClassify(tag)
		if closer {
			continue
		}
		switch kind {
		case dsmlBlock:
			f.openRegion(dsmlBlock, "")
		case dsmlInvoke:
			f.openRegion(dsmlInvoke, tag)
		}
	}
	if b.Len() == 0 && calls == nil {
		return "", nil, nil
	}
	return b.String(), calls, nil
}

// flush ends the stream. It publishes withheld prose, recovers the complete
// invokes of a markup region the provider never closed, and reports markup
// that yielded no call.
func (f *dsmlFilter) flush() (string, []ToolCall, error) {
	if !f.strip {
		return "", nil, nil
	}
	var calls []ToolCall
	if f.kind != dsmlNone {
		calls = f.closeRegion(len(f.region))
	}
	text := f.held
	f.held = ""
	if strings.Contains(text, "DSML") {
		// A truncated tag: protocol output, never prose.
		text = ""
	}
	if f.recover && f.blocks > 0 && f.calls == 0 {
		return text, calls, errDSMLUnrecovered
	}
	return text, calls, nil
}

func (f *dsmlFilter) openRegion(kind dsmlKind, opener string) {
	f.kind = kind
	f.blocks++
	f.scan = 0
	f.region = f.region[:0]
	f.region = append(f.region, opener...)
}

// closeRegion ends the open region at end, recovering its calls.
func (f *dsmlFilter) closeRegion(end int) []ToolCall {
	var found []ToolCall
	if f.recover {
		found = recoverDSMLCalls(string(f.region[:end]))
	}
	f.calls += len(found)
	f.region = f.region[:0]
	f.scan = 0
	f.kind = dsmlNone
	return found
}

// findCloser returns the offset just after the tag that closes the open
// region, or -1 when it has not arrived yet. An incomplete tag at the end of
// the buffer is left for the next delta.
func (f *dsmlFilter) findCloser() int {
	for {
		cut := bytes.IndexByte(f.region[f.scan:], '<')
		if cut < 0 {
			f.scan = len(f.region)
			return -1
		}
		start := f.scan + cut
		n, partial := dsmlTag(f.region[start:])
		if partial {
			f.scan = start
			return -1
		}
		if n == 0 {
			f.scan = start + 1
			continue
		}
		kind, closer, _ := dsmlClassify(string(f.region[start : start+n]))
		if closer && kind == f.kind {
			return start + n
		}
		f.scan = start + n
	}
}

// dsmlTag reports the length of a complete DSML tag at the start of s, or 0
// when s does not start with one. partial says s may still grow into a tag, so
// the caller must not publish it yet.
//
// It is generic so the same scan serves the text channel (string) and the
// buffered markup region ([]byte) without copying either.
func dsmlTag[S ~string | ~[]byte](s S) (n int, partial bool) {
	if len(s) == 0 || s[0] != '<' {
		return 0, false
	}
	i := 1
	skipSpace := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
	}
	// bar is the width of the bar at i, -1 when the delta split "\uff5c", and
	// 0 when s[i] is not a bar.
	bar := func() int {
		switch {
		case s[i] == '|':
			return 1
		case s[i] == 0xEF && len(s)-i >= 3 && s[i+1] == 0xBD && s[i+2] == 0x9C:
			return 3
		case s[i] == 0xEF && len(s)-i == 1:
			return -1
		case s[i] == 0xEF && len(s)-i == 2 && s[i+1] == 0xBD:
			return -1
		}
		return 0
	}
	run := func() (bars int, ok bool) {
		for {
			if i >= len(s) {
				return bars, false
			}
			w := bar()
			if w < 0 {
				return bars, false
			}
			if w == 0 {
				return bars, true
			}
			bars++
			i += w
			skipSpace()
		}
	}
	skipSpace()
	if i >= len(s) {
		return 0, true
	}
	if s[i] == '/' {
		i++
		skipSpace()
		if i >= len(s) {
			return 0, true
		}
	}
	bars, complete := run()
	if !complete {
		return 0, true
	}
	if bars == 0 {
		return 0, false
	}
	for k := 0; k < len("DSML"); k++ {
		if i >= len(s) {
			return 0, true
		}
		if s[i] != "DSML"[k] {
			return 0, false
		}
		i++
	}
	skipSpace()
	bars, complete = run()
	if !complete {
		return 0, true
	}
	if bars == 0 {
		return 0, false
	}
	for i < len(s) {
		if s[i] == '>' {
			return i + 1, false
		}
		i++
	}
	return 0, true
}

// dsmlClassify names what a complete tag is: a wrapper around a block, an
// invoke, or a stray tag that is protocol noise and is dropped.
func dsmlClassify(tag string) (kind dsmlKind, closer bool, name string) {
	body := strings.TrimSuffix(strings.TrimPrefix(tag, "<"), ">")
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(body, "/"); ok {
		closer = true
		body = strings.TrimSpace(rest)
	}
	body, bars := cutBars(body)
	if bars == 0 {
		return dsmlNone, closer, ""
	}
	rest, ok := strings.CutPrefix(body, "DSML")
	if !ok {
		return dsmlNone, closer, ""
	}
	body, bars = cutBars(rest)
	if bars == 0 {
		return dsmlNone, closer, ""
	}
	switch body {
	// "toolcalls" and "tool" are spellings production lanes have emitted for
	// "tool_calls"; recognizing them as a block keeps their contents out of the
	// text channel even when the call cannot be recovered.
	case "calls", "tool_calls", "function_calls", "toolcalls", "tool":
		return dsmlBlock, closer, ""
	case "invoke":
		return dsmlInvoke, closer, ""
	}
	if args, ok := strings.CutPrefix(body, "invoke"); ok && args != "" {
		if args[0] == ' ' || args[0] == '\t' {
			return dsmlInvoke, closer, dsmlAttribute(args, "name")
		}
	}
	return dsmlNone, closer, ""
}

// cutBars strips a leading run of bars and the spaces between and after them.
func cutBars(s string) (string, int) {
	for s != "" && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	bars := 0
	for {
		switch {
		case s == "":
			return s, bars
		case s[0] == '|':
			s = s[1:]
		case strings.HasPrefix(s, "｜"):
			s = s[len("｜"):]
		default:
			return s, bars
		}
		bars++
		for s != "" && (s[0] == ' ' || s[0] == '\t') {
			s = s[1:]
		}
	}
}

// dsmlAttribute reads name="value" out of a tag's attributes.
func dsmlAttribute(attrs, key string) string {
	for i := 0; i+len(key) <= len(attrs); i++ {
		if i > 0 && attrs[i-1] != ' ' && attrs[i-1] != '\t' {
			continue
		}
		if !strings.HasPrefix(attrs[i:], key) {
			continue
		}
		j := i + len(key)
		for j < len(attrs) && (attrs[j] == ' ' || attrs[j] == '\t') {
			j++
		}
		if j >= len(attrs) || attrs[j] != '=' {
			continue
		}
		j++
		for j < len(attrs) && (attrs[j] == ' ' || attrs[j] == '\t') {
			j++
		}
		if j >= len(attrs) {
			return ""
		}
		quote := attrs[j]
		if quote != '"' && quote != '\'' {
			return ""
		}
		end := strings.IndexByte(attrs[j+1:], quote)
		if end < 0 {
			return ""
		}
		return attrs[j+1 : j+1+end]
	}
	return ""
}

var (
	// The closers accept a space between "<" and "/" because dsmlTag does: a
	// lane may write the closing tag as "< / ｜ DSML ｜ invoke>", and the
	// scanner must not recognize a block whose calls it then cannot recover.
	dsmlInvokeRe = regexp.MustCompile(`(?s)<\s*[｜|][\s｜|]*DSML[\s｜|]*invoke\b([^>]*)>(.*?)<[ \t]*/[ \t]*[｜|][\s｜|]*DSML[\s｜|]*invoke[ \t]*>`)
	// dsmlParamOpenRe matches a parameter's opening tag only. A corrupted
	// structural token can lose or misspell the closing tag, so a value is
	// bounded by the next opener or the invoke's end, never by a paired match.
	dsmlParamOpenRe = regexp.MustCompile(`(?s)<\s*[｜|][\s｜|]*DSML[\s｜|]*parameter\b([^>]*)>`)
	// dsmlParamCloseRe matches any DSML closing tag: a closer whose name was
	// corrupted still ends the value it closes.
	dsmlParamCloseRe = regexp.MustCompile(`(?s)<[ \t]*/[ \t]*[｜|][\s｜|]*DSML\b[^>]*>`)
)

// recoverDSMLCalls turns the complete invokes of a markup region into calls.
func recoverDSMLCalls(markup string) []ToolCall {
	var calls []ToolCall
	for _, m := range dsmlInvokeRe.FindAllStringSubmatchIndex(markup, -1) {
		name := dsmlAttribute(markup[m[2]:m[3]], "name")
		if name == "" {
			continue
		}
		args, ok := dsmlArguments(markup[m[4]:m[5]])
		if !ok {
			continue
		}
		calls = append(calls, ToolCall{Name: name, Arguments: args})
	}
	return calls
}

// dsmlArguments builds the JSON object an invoke's parameters describe. An
// invoke without parameters carries the object itself.
//
// A parameter's value ends at its closer, at the next parameter's opener, or at
// the end of the invoke body. The middle case is how a lane that lost a closer
// still yields intact arguments: the damaged parameter cannot swallow the ones
// that follow it.
func dsmlArguments(body string) (string, bool) {
	if !dsmlParamOpenRe.MatchString(body) {
		raw := strings.TrimSpace(body)
		if raw == "" {
			return "{}", true
		}
		args, err := validateArgs(raw)
		return args, err == nil
	}
	var b strings.Builder
	b.WriteByte('{')
	comma := false
	for pos := 0; pos < len(body); {
		m := dsmlParamOpenRe.FindStringSubmatchIndex(body[pos:])
		if m == nil {
			break
		}
		attrs := body[pos+m[2] : pos+m[3]]
		rest := pos + m[1]
		next := dsmlParamOpenRe.FindStringIndex(body[rest:])
		close := dsmlParamCloseRe.FindStringIndex(body[rest:])
		var value string
		switch {
		case close != nil && (next == nil || close[0] < next[0]):
			value = body[rest : rest+close[0]]
			pos = rest + close[1]
		case next != nil:
			// The closer was lost or misspelled: the value ends at the next
			// opener, and the line break that formatted it is not part of it.
			value = strings.TrimRight(body[rest:rest+next[0]], " \t\r\n")
			pos = rest + next[0]
		default:
			value = strings.TrimRight(body[rest:], " \t\r\n")
			pos = len(body)
		}
		if name := dsmlAttribute(attrs, "name"); name != "" {
			if comma {
				b.WriteByte(',')
			}
			comma = true
			key, _ := json.Marshal(name)
			b.Write(key)
			b.WriteByte(':')
			b.WriteString(dsmlValue(value, dsmlAttribute(attrs, "string") == "true"))
		}
	}
	b.WriteByte('}')
	args, err := validateArgs(b.String())
	return args, err == nil
}

// dsmlValue encodes one parameter. string="true" means the text is the value;
// otherwise it is JSON, kept as text when it does not decode so the tool still
// gets something.
func dsmlValue(value string, literal bool) string {
	if !literal {
		if raw := strings.TrimSpace(value); raw != "" && json.Valid([]byte(raw)) {
			return raw
		}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

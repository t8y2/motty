package workbench

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
)

// noteHost is the identity and cached display data carried by a host tag.
type noteHost struct{ CatalogID, Address, User, Label string }

func noteHostFor(h Host) noteHost { return noteHost{h.CatalogID, h.Address, h.User, h.Label} }
func noteHostLabel(h noteHost) string {
	if h.Label != "" {
		return h.Label
	}
	if h.Address != "" {
		return h.Address
	}
	return h.CatalogID
}
func encodeNoteHost(h noteHost) string {
	fields := []string{h.CatalogID, h.Address, h.User, h.Label}
	for i, value := range fields {
		var b strings.Builder
		for _, ch := range []byte(value) {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~", rune(ch)) {
				b.WriteByte(ch)
			} else {
				fmt.Fprintf(&b, "%%%02X", ch)
			}
		}
		fields[i] = b.String()
	}
	return "{@Host:" + fields[0] + "@Address:" + fields[1] + "@User:" + fields[2] + "@Label:" + fields[3] + "@}"
}
func parseNoteHost(tag string) (noteHost, bool) {
	var h noteHost
	if !strings.HasPrefix(tag, "{@Host:") || !strings.HasSuffix(tag, "@}") {
		return h, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(tag, "{@Host:"), "@}"), "@")
	if len(parts) != 4 {
		return h, false
	}
	prefixes := []string{"", "Address:", "User:", "Label:"}
	values := []*string{&h.CatalogID, &h.Address, &h.User, &h.Label}
	for i, part := range parts {
		if !strings.HasPrefix(part, prefixes[i]) {
			return noteHost{}, false
		}
		raw := strings.TrimPrefix(part, prefixes[i])
		// Only unreserved bytes and percent escapes belong in an encoded field.
		for j := 0; j < len(raw); j++ {
			ch := raw[j]
			if ch == '%' {
				if j+2 >= len(raw) {
					return noteHost{}, false
				}
				j += 2
				continue
			}
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~", rune(ch))) {
				return noteHost{}, false
			}
		}
		decoded, err := url.PathUnescape(raw)
		if err != nil || !utf8.ValidString(decoded) {
			return noteHost{}, false
		}
		*values[i] = decoded
	}
	return h, h.CatalogID != ""
}
func noteHasHost(tags []string, id string) bool {
	for _, tag := range tags {
		if h, ok := parseNoteHost(tag); ok && h.CatalogID == id {
			return true
		}
	}
	return false
}
func (s *State) noteHostByID(id string) (noteHost, bool) {
	for _, h := range s.hosts {
		if h.CatalogID == id {
			return noteHostFor(h), true
		}
	}
	return noteHost{CatalogID: id}, false
}
func (s *State) normalizeNoteHosts(n savedNote) savedNote {
	tags := make([]string, 0, len(n.Tags))
	seen := map[string]bool{}
	groupSeen := map[string]bool{}
	for _, tag := range n.Tags {
		if h, ok := parseNoteHost(tag); ok {
			if seen[h.CatalogID] {
				continue
			}
			seen[h.CatalogID] = true
			tag = encodeNoteHost(h)
		}
		if path, ok := parseNoteHostGroup(tag); ok {
			key := path
			if groupSeen[key] {
				continue
			}
			groupSeen[key] = true
			tag = encodeNoteHostGroup(path)
		}
		tags = append(tags, tag)
	}
	n.Tags = tags
	return n
}
func (s *State) replaceNoteTagEditor(tags []string) {
	s.noteContent().tags.SetSelection(text.Range{End: s.noteContent().tags.RuneCount()})
	s.noteContent().tags.Insert(strings.Join(tags, ", "))
}
func (s *State) toggleNoteHost(id string) {
	if s.noteIndex(s.noteContent().selected) < 0 {
		return
	}
	tags := noteWords(s.noteContent().tags.String())
	if noteHasHost(tags, id) {
		tags = removeNoteHost(tags, id)
	} else {
		h, exists := s.noteHostByID(id)
		if !exists {
			return
		}
		tags = append(tags, encodeNoteHost(h))
	}
	s.replaceNoteTagEditor(tags)
	s.save()
}
func removeNoteHost(tags []string, id string) []string {
	return slices.DeleteFunc(slices.Clone(tags), func(tag string) bool { h, ok := parseNoteHost(tag); return ok && h.CatalogID == id })
}

type noteHostUpdate struct{ noteID, before, after string }

func scanNoteHosts(notes []savedNote, hosts map[string]noteHost) []noteHostUpdate {
	var updates []noteHostUpdate
	for _, n := range notes {
		for _, tag := range n.Tags {
			h, ok := parseNoteHost(tag)
			if !ok {
				continue
			}
			current, exists := hosts[h.CatalogID]
			if !exists {
				continue
			}
			if next := encodeNoteHost(current); next != tag {
				updates = append(updates, noteHostUpdate{n.ID, tag, next})
			}
		}
	}
	return updates
}
func (s *State) refreshNoteHosts() {
	n := &s.notes
	n.hostRefreshNeeded = false
	if n.hostScanRunning || len(n.hostUpdates) > 0 {
		n.hostScanAgain = true
		return
	}
	notes := make([]savedNote, len(n.list))
	for i, note := range n.list {
		notes[i] = note
		notes[i].Tags = slices.Clone(s.noteTagsForID(note.ID))
	}
	if len(notes) == 0 {
		return
	}
	hosts := map[string]noteHost{}
	for _, host := range s.hosts {
		hosts[host.CatalogID] = noteHostFor(host)
	}
	n.hostScanRunning = true
	invalidate := s.options.Invalidate
	go func() {
		updates := scanNoteHosts(notes, hosts)
		s.aiPostWithInvalidator(func(owner *State) {
			if owner.closed {
				return
			}
			owner.notes.hostScanRunning = false
			owner.notes.hostUpdates = updates
			owner.applyNoteHostUpdates()
		}, invalidate)
	}()
}
func (s *State) applyNoteHostUpdates() {
	n := &s.notes
	if s.noteComposing() {
		return
	}
	changed := false
	for _, update := range n.hostUpdates {
		at := s.noteIndex(update.noteID)
		if at < 0 {
			continue
		}
		wanted, _ := parseNoteHost(update.after)
		current, exists := s.noteHostByID(wanted.CatalogID)
		if !exists {
			continue
		}
		if encodeNoteHost(current) != update.after {
			n.hostScanAgain = true
			continue
		}
		tags := s.noteTagsForID(update.noteID)
		index := slices.Index(tags, update.before)
		if index < 0 {
			continue
		}
		tags = slices.Clone(tags)
		tags[index] = update.after
		s.updateNoteBufferTags(update.noteID, tags)
		changed = true
	}
	n.hostUpdates = nil
	if changed {
		s.save()
	}
	if n.hostScanAgain {
		n.hostScanAgain = false
		s.refreshNoteHosts()
	}
}

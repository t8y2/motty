package workbench

import (
	"slices"
	"strings"
)

func encodeNoteHostGroup(path string) string {
	encoded := encodeNoteHost(noteHost{CatalogID: path})
	return "{@HostGroup:" + strings.Split(strings.TrimPrefix(encoded, "{@Host:"), "@")[0] + "@}"
}
func parseNoteHostGroup(tag string) (string, bool) {
	if !strings.HasPrefix(tag, "{@HostGroup:") || !strings.HasSuffix(tag, "@}") {
		return "", false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(tag, "{@HostGroup:"), "@}")
	h, ok := parseNoteHost("{@Host:" + raw + "@Address:@User:@Label:@}")
	return h.CatalogID, ok
}
func noteSpecialTag(tag string) bool {
	_, h := parseNoteHost(tag)
	_, g := parseNoteHostGroup(tag)
	return h || g
}
func noteHasGroup(tags []string, path string) bool {
	for _, tag := range tags {
		if p, ok := parseNoteHostGroup(tag); ok && p == path {
			return true
		}
	}
	return false
}
func (s *State) noteGroupExists(path string) bool {
	if path == "" {
		return false
	}
	for _, g := range s.hostGroups {
		if hostGroupWithin(g.Name, path) {
			return true
		}
	}
	for _, h := range s.hosts {
		if hostGroupWithin(strings.TrimSpace(h.Group), path) {
			return true
		}
	}
	return false
}
func (s *State) noteMatchesHost(tags []string, id string) bool {
	if noteHasHost(tags, id) {
		return true
	}
	for _, h := range s.hosts {
		if h.CatalogID != id {
			continue
		}
		for _, tag := range tags {
			if path, ok := parseNoteHostGroup(tag); ok && hostGroupWithin(strings.TrimSpace(h.Group), path) {
				return true
			}
		}
	}
	return false
}
func (s *State) invalidateNoteGroup(path string) {
	identities := map[string]bool{}
	seen := map[string]bool{}
	for _, job := range s.notes.groupJobs {
		for _, u := range job.updates {
			if u.noteID != s.noteContent().selected || seen[u.identity] {
				continue
			}
			seen[u.identity] = true
			if u.before == encodeNoteHostGroup(path) {
				identities[u.identity] = true
			}
		}
	}
	for _, job := range s.notes.groupJobs {
		for i := range job.updates {
			u := &job.updates[i]
			if u.noteID == s.noteContent().selected && identities[u.identity] {
				u.cancelled = true
			}
		}
	}
}
func (s *State) removeNoteGroup(path string) {
	s.invalidateNoteGroup(path)
	tags := slices.DeleteFunc(noteWords(s.noteContent().tags.String()), func(tag string) bool { p, ok := parseNoteHostGroup(tag); return ok && p == path })
	s.replaceNoteTagEditor(tags)
	s.save()
}
func (s *State) toggleNoteGroup(path string) {
	if s.noteIndex(s.noteContent().selected) < 0 || path == "" {
		return
	}
	tags := noteWords(s.noteContent().tags.String())
	if noteHasGroup(tags, path) {
		s.removeNoteGroup(path)
		return
	}
	if !s.noteGroupExists(path) {
		return
	}
	s.invalidateNoteGroup(path)
	s.replaceNoteTagEditor(append(tags, encodeNoteHostGroup(path)))
	s.save()
}

type noteGroupUpdate struct {
	noteID, before, identity string
	cancelled                bool
}
type noteGroupJob struct {
	updates      []noteGroupUpdate
	path, target string
	deleted      bool
}

func (job *noteGroupJob) replacement(tag string) string {
	path, ok := parseNoteHostGroup(tag)
	if !ok || !hostGroupWithin(path, job.path) {
		return tag
	}
	if job.deleted {
		return ""
	}
	return encodeNoteHostGroup(job.target + strings.TrimPrefix(path, job.path))
}

// Capture association identities at the catalog operation, including queued renames.
func (s *State) queueNoteGroupChange(path, target string, deleted bool) {
	job := &noteGroupJob{path: path, target: target, deleted: deleted}
	for _, note := range s.notes.list {
		tags := s.noteTagsForID(note.ID)
		for _, tag := range tags {
			if _, ok := parseNoteHostGroup(tag); !ok {
				continue
			}
			projected, identity := tag, tag
			for _, pending := range s.notes.groupJobs {
				for _, u := range pending.updates {
					if !u.cancelled && u.noteID == note.ID && u.before == projected {
						projected = pending.replacement(u.before)
						identity = u.identity
						break
					}
				}
			}
			p, ok := parseNoteHostGroup(projected)
			if !ok || !hostGroupWithin(p, path) {
				continue
			}
			job.updates = append(job.updates, noteGroupUpdate{noteID: note.ID, before: projected, identity: identity})
		}
	}
	if len(job.updates) == 0 {
		return
	}
	s.notes.groupJobs = append(s.notes.groupJobs, job)
	s.runNoteGroupJobs()
}
func (s *State) runNoteGroupJobs() {
	if len(s.notes.groupJobs) == 0 || s.noteComposing() {
		return
	}
	for _, job := range s.notes.groupJobs {
		for _, u := range job.updates {
			if s.noteIndex(u.noteID) < 0 || u.cancelled {
				continue
			}
			tags := s.noteTagsForID(u.noteID)
			idx := slices.Index(tags, u.before)
			if idx < 0 {
				continue
			}
			tags = slices.Clone(tags)
			if after := job.replacement(u.before); after == "" {
				tags = slices.Delete(tags, idx, idx+1)
			} else {
				tags[idx] = after
			}
			s.updateNoteBufferTags(u.noteID, s.normalizeNoteHosts(savedNote{Tags: tags}).Tags)
		}
	}
	s.notes.groupJobs = nil
}

package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	layoutcore "motty/internal/workspace"
)

const (
	maxWorkspaceTemplateNameRunes = 80
	maxWorkspaceTemplateHosts     = layoutcore.MaxPanes
)

var (
	ErrWorkspaceTemplateName       = errors.New("workbench: workspace template needs a name")
	ErrWorkspaceTemplateHosts      = errors.New("workbench: workspace template needs 1 to 12 hosts")
	ErrWorkspaceTemplateHostID     = errors.New("workbench: workspace template host id is invalid")
	ErrWorkspaceTemplateDupID      = errors.New("workbench: workspace template host ids must be unique")
	ErrWorkspaceTemplateLayout     = errors.New("workbench: workspace template layout is invalid")
	ErrWorkspaceTemplateID         = errors.New("workbench: workspace template id is invalid")
	ErrWorkspaceSaveEmptyPane      = errors.New("workbench: workspace has an empty pane")
	ErrWorkspaceSaveMissingSession = errors.New("workbench: workspace pane has no session")
	ErrWorkspaceSaveMissingHost    = errors.New("workbench: workspace pane has no stable host reference")
	ErrWorkspaceSaveDuplicateHost  = errors.New("workbench: workspace panes repeat a host")
)

// WorkspaceTemplate is the small, shareable definition of a saved workspace.
// It references catalog hosts or this computer's default local terminal and
// never contains credentials, sessions, or terminal state.
type WorkspaceTemplate struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	HostIDs []string              `json:"host_ids"`
	Layout  layoutcore.LayoutSpec `json:"layout"`
	// Columns is read from files written before layouts existed, which stored
	// only a grid width. normalizeWorkspaceTemplate folds it into Layout.
	Columns uint8 `json:"columns,omitempty"`
}

// NewWorkspaceTemplate validates a new template and assigns its stable id.
func NewWorkspaceTemplate(name string, hostIDs []string, layout layoutcore.LayoutSpec) (WorkspaceTemplate, error) {
	template := WorkspaceTemplate{
		Name: name, HostIDs: slices.Clone(hostIDs), Layout: layout,
	}
	if err := template.validate(); err != nil {
		return WorkspaceTemplate{}, err
	}
	id, err := newWorkspaceTemplateID()
	if err != nil {
		return WorkspaceTemplate{}, err
	}
	template.ID = id
	return template, nil
}

// validate normalizes and checks every field except the id.
func (template *WorkspaceTemplate) validate() error {
	template.Name = strings.TrimSpace(template.Name)
	if !validWorkspaceTemplateName(template.Name) {
		return ErrWorkspaceTemplateName
	}
	if len(template.HostIDs) == 0 || len(template.HostIDs) > maxWorkspaceTemplateHosts {
		return ErrWorkspaceTemplateHosts
	}
	if template.Layout.Validate(len(template.HostIDs)) != nil {
		return ErrWorkspaceTemplateLayout
	}
	seen := make(map[string]struct{}, len(template.HostIDs))
	for index, hostID := range template.HostIDs {
		hostID = strings.TrimSpace(hostID)
		if hostID != workspaceLocalTerminalRef && (!validCatalogID(hostID) || hostID == "") {
			return ErrWorkspaceTemplateHostID
		}
		if _, exists := seen[hostID]; exists {
			return ErrWorkspaceTemplateDupID
		}
		seen[hostID] = struct{}{}
		template.HostIDs[index] = hostID
	}
	return nil
}

// Validate checks a template loaded from persistence, including its id.
func (template WorkspaceTemplate) Validate() error {
	_, err := normalizeWorkspaceTemplate(template)
	return err
}

// normalizeWorkspaceTemplate validates a stored template and returns the
// normalized value persistence should keep instead of discarding it.
func normalizeWorkspaceTemplate(template WorkspaceTemplate) (WorkspaceTemplate, error) {
	if !validWorkspaceTemplateID(template.ID) {
		return WorkspaceTemplate{}, ErrWorkspaceTemplateID
	}
	template.HostIDs = slices.Clone(template.HostIDs)
	if template.Layout.Kind == 0 && template.Columns != 0 {
		template.Layout = layoutcore.GridSpec(template.Columns)
	}
	template.Columns = 0
	if err := template.validate(); err != nil {
		return WorkspaceTemplate{}, err
	}
	return template, nil
}

func validWorkspaceTemplateID(id string) bool {
	if len(id) != len("ws_")+32 || !strings.HasPrefix(id, "ws_") {
		return false
	}
	_, err := hex.DecodeString(id[len("ws_"):])
	return err == nil
}

func newWorkspaceTemplateID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("workbench: generate workspace template id: %w", err)
	}
	return "ws_" + hex.EncodeToString(raw[:]), nil
}

func validWorkspaceTemplateName(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= maxWorkspaceTemplateNameRunes &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) })
}

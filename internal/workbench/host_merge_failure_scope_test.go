package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"motty/internal/vault"
)

func TestMixedHostMergesKeepNonVNCReferencesWhenOneVaultRecordIsCorrupt(t *testing.T) {
	for _, protocol := range []string{"ssh", "rdp"} {
		t.Run(protocol, func(t *testing.T) {
			options := testOptions(t)
			options.Keystore = &memKeys{}
			initial := newTestState(t, options)
			nonVNCSource := hostSecretName("host_terminal_b", hostPasswordPart)
			vncSource := hostSecretName("host_vnc_b", vncSystemPasswordPart("pi"))
			if err := initial.secrets.vault.Set(nonVNCSource, "saved terminal password"); err != nil {
				t.Fatal(err)
			}
			if err := initial.secrets.vault.Set(vncSource, "saved Pi password"); err != nil {
				t.Fatal(err)
			}
			initial.Close()
			vaultPath := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
			vault.Forget(vaultPath)
			raw, err := os.ReadFile(vaultPath)
			if err != nil {
				t.Fatal(err)
			}
			var file struct {
				Version int `json:"version"`
				Records []struct {
					ID   []byte `json:"id"`
					Data []byte `json:"data"`
				} `json:"records"`
			}
			if err := json.Unmarshal(raw, &file); err != nil {
				t.Fatal(err)
			}
			if len(file.Records) != 2 || len(file.Records[0].Data) == 0 {
				t.Fatalf("fixture needs the two passwords in insertion order: %+v", file)
			}
			// Change only the first password's authentication tag. The VNC
			// account remains readable, while Names cannot enumerate this vault.
			file.Records[0].Data[len(file.Records[0].Data)-1] ^= 1
			raw, err = json.Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(vaultPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			readable, err := vault.Open(vaultPath, options.Keystore)
			if err != nil {
				t.Fatal(err)
			}
			if value, found, err := readable.Get(vncSource); err != nil || !found || value != "saved Pi password" {
				t.Fatalf("the VNC source must remain intact: value=%q found=%v err=%v", value, found, err)
			}
			if _, _, err := readable.Get(nonVNCSource); !errors.Is(err, vault.ErrCorrupt) {
				t.Fatalf("the terminal source must report the damaged record: %v", err)
			}
			if _, err := readable.Names(); !errors.Is(err, vault.ErrCorrupt) {
				t.Fatalf("enumerating accounts must report the damaged record: %v", err)
			}
			vault.Forget(vaultPath)

			port := uint16(22)
			if protocol == "rdp" {
				port = 3389
			}
			terminalTransport := savedTransport{Protocol: protocol}
			vncTransport := savedTransport{Protocol: "vnc"}
			template, err := normalizeWorkspaceTemplate(WorkspaceTemplate{
				ID: "ws_0123456789abcdef0123456789abcdef", Name: "Mixed servers", Columns: 2,
				HostIDs: []string{"host_terminal_b", "host_vnc_b"},
			})
			if err != nil {
				t.Fatal(err)
			}
			saved := savedState{
				Hosts: []savedHost{
					{ID: "host_terminal_b", Label: "Terminal", Address: "terminal.example.com", User: "ops", Port: port, savedTransport: terminalTransport},
					{ID: "host_terminal_a", Label: "Terminal", Address: "terminal.example.com", User: "ops", Port: port, savedTransport: terminalTransport},
					{ID: "host_vnc_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: vncTransport},
					{ID: "host_vnc_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: vncTransport},
					{ID: "host_jump", Label: "Jump", Address: "jump.example.com", User: "ops", Port: 22},
				},
				Sessions: []savedSession{
					{Label: "Terminal", Address: "terminal.example.com", User: "ops", Port: port, SavedHost: true, CatalogID: "host_terminal_b", savedTransport: terminalTransport},
					{Label: "Pi", Address: "pi.example.com", Port: 5900, SavedHost: true, CatalogID: "host_vnc_b", savedTransport: vncTransport},
				},
				WorkspaceTemplates: []WorkspaceTemplate{template},
			}
			if protocol == "ssh" {
				saved.AgentHosts = []string{"host_terminal_b"}
				saved.Proxies = []savedProxy{{
					ID: 1, TargetID: "host_terminal_b", JumpID: "host_jump", TargetAddress: "terminal.example.com", TargetUser: "ops", TargetPort: port,
					JumpAddress: "jump.example.com", JumpUser: "ops", JumpPort: 22,
				}}
			}
			if err := saveState(options.StatePath, saved); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				state := newTestState(t, options)
				var hostIDs []string
				for _, host := range state.hosts {
					hostIDs = append(hostIDs, host.CatalogID)
				}
				slices.Sort(hostIDs)
				if !slices.Equal(hostIDs, []string{"host_jump", "host_terminal_a", "host_vnc_a", "host_vnc_b"}) {
					t.Fatalf("restart %d merged the wrong hosts: %v", restart, hostIDs)
				}
				if restart == 0 && (len(state.merged) != 1 || state.merged["host_terminal_b"] != "host_terminal_a" || !state.mergedHosts) {
					t.Fatalf("only the terminal merge should produce aliases: %v merged=%v", state.merged, state.mergedHosts)
				}
				if len(state.sessions) != 2 || state.sessions[0].host.CatalogID != "host_terminal_a" || state.sessions[1].host.CatalogID != "host_vnc_b" {
					t.Fatalf("restart %d moved a saved tab to the wrong host: %+v", restart, state.sessions)
				}
				if protocol == "ssh" {
					if len(state.proxies) != 1 {
						t.Fatalf("restart %d lost the saved SSH route: %+v", restart, state.proxies)
					}
					target, found := state.hostByID(state.proxies[0].TargetID)
					jump, jumpFound := state.hostByID(state.proxies[0].JumpID)
					if !found || target.CatalogID != "host_terminal_a" || !jumpFound || jump.CatalogID != "host_jump" {
						t.Fatalf("restart %d changed SSH route endpoints: target=%+v jump=%+v", restart, target, jump)
					}
				}
				if len(state.templates) != 1 || !slices.Equal(state.templates[0].HostIDs, []string{"host_terminal_a", "host_vnc_b"}) {
					t.Fatalf("restart %d changed workspace host references: %+v", restart, state.templates)
				}
				if protocol == "ssh" && (!state.agentHosts["host_terminal_a"] || state.agentHosts["host_terminal_b"]) {
					t.Fatalf("restart %d lost agent access to the surviving SSH host: %v", restart, state.agentHosts)
				}
				if value, found, err := state.secrets.vault.Get(vncSource); err != nil || !found || value != "saved Pi password" {
					t.Fatalf("restart %d changed the VNC source password: %q %v %v", restart, value, found, err)
				}
				if _, found, err := state.secrets.vault.Get(hostSecretName("host_vnc_a", vncSystemPasswordPart("pi"))); err != nil || found {
					t.Fatalf("restart %d migrated a VNC account despite the failed merge: found=%v err=%v", restart, found, err)
				}
				if len(state.fileNotices.items) != 1 || !strings.Contains(state.fileNotices.items[0].detail, state.vaultErrorText(vault.ErrCorrupt)) {
					t.Fatalf("restart %d lost the original Vault failure: %+v", restart, state.fileNotices.items)
				}
				state.Close()
				vault.Forget(vaultPath)
			}
		})
	}
}

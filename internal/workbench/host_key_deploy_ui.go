package workbench

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

var (
	hitHostKeyManage       = tree.ID(shellHits, "host-key-manage")
	hitHostKeyDeployCancel = tree.ID(dialogHits, "host-key-deploy-cancel")
	hitHostKeyDeployClose  = tree.ID(dialogHits, "host-key-deploy-close")
	hitHostKeyDeployNext   = tree.ID(dialogHits, "host-key-deploy-next")
	hitHostKeyDeployBack   = tree.ID(dialogHits, "host-key-deploy-back")
	hitHostKeyDeployApply  = tree.ID(dialogHits, "host-key-deploy-apply")
	hitHostKeyDeploySwitch = tree.ID(dialogHits, "host-key-deploy-switch")
	hitHostKeyDeployKeys   = tree.Sub(dialogHits, tree.Name("host-key-deploy-key"))
)

type hostKeyDeployView struct {
	targets                      []Host
	keyID                        uint32
	confirmedKey                 identityRecord
	switchAuth, confirm, started bool
	changed                      bool
	list                         ui.ListState
}

func (state *State) deployText(en, zh, tw string) string {
	switch state.language {
	case i18n.Chinese:
		return zh
	case i18n.TraditionalChinese:
		return tw
	default:
		return en
	}
}

func (state *State) hostKeyManagementLabel() string {
	return state.deployText("Key management", "密钥管理", "金鑰管理")
}

func usableDeployIdentity(key identityRecord) bool {
	return key.Stored && key.Path != "" && strings.TrimSpace(key.PublicKey) != "" && key.Error == "" &&
		!key.SecurityKey && !key.Certificate
}

func (state *State) openHostKeyManagement() {
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled {
		return
	}
	view := hostKeyDeployView{started: state.hostKeyDeploy.running}
	if state.hostKeyDeploy.running && len(state.hostKeyDeployView.targets) > 0 {
		view.targets = state.hostKeyDeployView.targets
		view.switchAuth = state.hostKeyDeployView.switchAuth
	} else {
		seen := make(map[uint32]bool, len(state.hostSelections))
		for _, id := range state.hostSelections {
			if !seen[id] {
				if host, ok := state.hostByID(id); ok {
					view.targets = append(view.targets, host)
				}
				seen[id] = true
			}
		}
	}
	state.hostKeyDeployView = view
	state.modal = modalHostKeyDeploy
}

func validDeployTargets(targets []Host) bool {
	if len(targets) == 0 {
		return false
	}
	for _, host := range targets {
		if !host.isSSH() || host.mosh() || host.InteractiveOnly || host.tunnel || host.System == sshconn.SystemWindows || host.CatalogID == "" || host.Address == "" {
			return false
		}
	}
	return true
}

func (state *State) deployKeySelected() (identityRecord, bool) {
	key, ok := state.identityByID(state.hostKeyDeployView.keyID)
	return key, ok && usableDeployIdentity(key)
}

func (state *State) deployTargetsUnchanged(targets []Host) bool {
	for _, target := range targets {
		current, ok := state.hostByID(target.ID)
		if !ok || !reflect.DeepEqual(current, target) {
			return false
		}
	}
	return true
}

func (state *State) clickHostKeyManagement(id gfx.ID) bool {
	if id == hitHostKeyManage {
		state.openHostKeyManagement()
		return true
	}
	if state.modal != modalHostKeyDeploy {
		return false
	}
	v := &state.hostKeyDeployView
	switch {
	case id == hitHostKeyDeployClose:
		if !state.hostKeyDeploy.running {
			state.closeModal()
		}
	case id == hitHostKeyDeployCancel:
		if state.hostKeyDeploy.running && !state.hostKeyDeploy.canceled {
			state.cancelHostKeyDeployment()
		}
	case id == hitHostKeyDeployBack:
		if !v.started {
			v.confirm = false
			v.list.ScrollTo(0, ui.ListStart)
		}
	case id == hitHostKeyDeploySwitch:
		if !v.confirm && !v.started && validDeployTargets(v.targets) {
			v.switchAuth = !v.switchAuth
		}
	case id.Scope() == hitHostKeyDeployKeys:
		if !v.confirm && !v.started {
			if key, ok := state.identityByID(uint32(item(id))); ok && usableDeployIdentity(key) {
				v.keyID = key.ID
			}
		}
	case id == hitHostKeyDeployNext:
		if !v.confirm && !v.started && !v.changed && validDeployTargets(v.targets) {
			if key, ok := state.deployKeySelected(); ok {
				v.confirmedKey = key
				v.confirm = true
				v.list.ScrollTo(0, ui.ListStart)
			}
		}
	case id == hitHostKeyDeployApply:
		if !v.confirm || v.started || v.changed || state.hostKeyDeploy.running || !validDeployTargets(v.targets) {
			return true
		}
		key, ok := state.deployKeySelected()
		if !ok || key != v.confirmedKey || !state.deployTargetsUnchanged(v.targets) {
			v.changed = true
			return true
		}
		ids := make([]uint32, len(v.targets))
		for i, h := range v.targets {
			ids[i] = h.ID
		}
		v.started = true
		v.list.ScrollTo(0, ui.ListStart)
		state.startHostKeyDeployment(ids, key.Path, v.switchAuth)
	}
	return true
}

func (state *State) hostKeyDeployKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalHostKeyDeploy || event.Release || event.Key != gfx.KeyEscape {
		return gfx.EventResult{}
	}
	if state.hostKeyDeploy.running {
		return gfx.EventResult{Handled: true}
	}
	if state.hostKeyDeployView.confirm && !state.hostKeyDeployView.started {
		state.clickHostKeyManagement(hitHostKeyDeployBack)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitHostKeyDeployNext}
	}
	state.closeModal()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitHostKeyManage}
}

func (state *State) declareHostKeyDeployment(c ui.C) {
	if state.modal != modalHostKeyDeploy {
		return
	}
	v, t := &state.hostKeyDeployView, state.deployText
	title := t("Batch import SSH public key", "批量导入 SSH 公钥", "批次匯入 SSH 公鑰")
	key, hasKey := state.deployKeySelected()
	var keys []identityRecord
	if !v.confirm && !v.started {
		for _, k := range state.identities {
			if usableDeployIdentity(k) {
				keys = append(keys, k)
			}
		}
	}
	running := state.hostKeyDeploy.running
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 680, Height: min(590, max(340, dialogViewport(c).Height-80)),
		Layer: dialogLayer, Scope: dialogScope, NoClose: true, CloseID: hitHostKeyDeployClose,
	})
	dialog.Body(func() {
		body := ui.Column(c).Fill().Gap(8)
		body.Children(func() {
			description := t("Select a key to add its public half to the selected accounts' OpenSSH authorized_keys. No server password settings are changed. For an unknown fingerprint, connect normally and verify it first.",
				"选择密钥后，仅将公钥写入所选账号的 OpenSSH authorized_keys，不修改服务器密码登录设置。遇到未知主机指纹，请先按普通流程连接并核实指纹。", "選擇金鑰後，僅將公鑰寫入所選帳號的 OpenSSH authorized_keys，不修改伺服器密碼登入設定。遇到未知主機指紋，請先按一般流程連線並核實指紋。")
			if v.started {
				if running && state.hostKeyDeploy.canceled {
					description = t("Stopping the remaining operations. Keys already installed stay on the server; no pending local profile switches will be applied.", "正在停止剩余操作。已经写入的公钥会保留，不再执行待处理的本地认证切换。", "正在停止剩餘操作。已經寫入的公鑰會保留，不再執行待處理的本機驗證切換。")
				} else if running {
					description = t("Import in progress. Each host is reported separately.", "正在导入，将逐台显示结果。", "正在匯入，將逐台顯示結果。")
				} else {
					description = t("Import results by host. A partial failure does not undo successful imports.", "以下是逐台执行结果。个别主机失败不会撤销已成功的导入。", "以下是逐台執行結果。個別主機失敗不會撤銷已成功的匯入。")
				}
			} else if v.confirm {
				description = fmt.Sprintf(t("Review and confirm: %s · %d host(s).", "请核对并确认：%s · %d 台主机。", "請核對並確認：%s · %d 台主機。"), v.confirmedKey.Name+" · "+v.confirmedKey.Fingerprint, len(v.targets))
			}
			ui.Paragraph(c, description, 4).Color(state.pal.muted).FillWidth().Shrink(0)
			switch {
			case v.changed:
				ui.Paragraph(c, t("A host or key changed. Close this dialog and review your selection again.", "主机或密钥已变化。请关闭窗口后重新检查选择。", "主機或金鑰已變更。請關閉視窗後重新檢查選擇。"), 2).Color(state.pal.danger).FillWidth().Shrink(0)
			case !v.started && len(v.targets) == 0:
				ui.Paragraph(c, t("Check one or more SSH hosts in the host list first.", "请先在主机列表中勾选至少一台 SSH 主机。", "請先在主機清單中勾選至少一台 SSH 主機。"), 2).Color(state.pal.muted).FillWidth().Shrink(0)
			case !v.started && !validDeployTargets(v.targets):
				ui.Paragraph(c, t("Selection contains unsupported hosts. Select only saved SSH hosts that allow file operations.", "选择包含不支持的主机。请选择允许文件操作的已保存 SSH 主机。", "選擇包含不支援的主機。請選擇允許檔案操作的已儲存 SSH 主機。"), 2).Color(state.pal.danger).FillWidth().Shrink(0)
			case !v.started && !v.confirm && len(keys) == 0:
				ui.Paragraph(c, t("No usable stored SSH key. Add a supported key in SSH Keys first.", "没有可用的已存密钥，请先到 SSH 密钥链添加支持的密钥。", "沒有可用的已儲存金鑰，請先到 SSH 金鑰鏈新增支援的金鑰。"), 2).Color(state.pal.muted).FillWidth().Shrink(0)
			}
			if !v.started && !v.confirm {
				label := t("Import and switch Motty to key authentication", "批量导入并切换为密钥访问", "批次匯入並切換為金鑰存取")
				ui.Checkbox(c, state.pageFlag(v.switchAuth), label).Hit(hitHostKeyDeploySwitch, label).Disabled(!validDeployTargets(v.targets)).Shrink(0)
			}
			if (v.confirm || v.started) && v.switchAuth {
				ui.Paragraph(c, t("Switch only Motty's saved profile after the new key successfully logs in.", "只有新密钥登录验证通过后，才切换 Motty 本地的主机认证方式。", "只有新金鑰登入驗證通過後，才切換 Motty 本機的主機驗證方式。"), 2).Color(state.pal.muted).FillWidth().Shrink(0)
			}
			results := make(map[uint32]hostKeyDeployResult, len(state.hostKeyDeploy.results))
			if v.started {
				for _, r := range state.hostKeyDeploy.results {
					results[r.HostID] = r
				}
			}
			count := len(keys) + len(v.targets)
			if count == 0 {
				return
			}
			ui.List(c, &v.list, count, func(row ui.ListRow) {
				row.Element.Height(59).PadX(3).Items(ui.Center).Children(func() {
					if row.Index < len(keys) {
						entry := keys[row.Index]
						label := entry.Name + " · " + entry.Fingerprint
						if entry.ID == key.ID {
							label = "✓ " + label
						}
						ui.Button(row.C, label).FillWidth().Hit(hitHostKeyDeployKeys.At(int(entry.ID)), label)
						return
					}
					host := v.targets[row.Index-len(keys)]
					label := state.hostPrivacySemanticLabel(host)
					if label == "" {
						if state.hostPrivacy.addressHidden() {
							label = fmt.Sprintf("#%d", host.ID)
						} else {
							label = host.Address
						}
					}
					if !v.started {
						login := state.hostCatalog.login(host)
						if state.hostPrivacy.userHidden() {
							login = "•••"
						}
						address := net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
						if host.Port == 0 {
							address = net.JoinHostPort(host.Address, "22")
						}
						if state.hostPrivacy.addressHidden() {
							address = "•••"
						}
						state.declareHostKeyDeployLine(row.C, label, login+"@"+address)
						return
					}
					result, ok := results[host.ID]
					msg := t("Queued or processing", "等待或正在处理", "等待或正在處理")
					if ok {
						msg = t("Not imported", "未导入", "未匯入")
						if result.Imported {
							msg = t("Public key ready", "公钥已就绪", "公鑰已就緒")
						}
						if result.Switched {
							msg = t("Key ready and authentication switched", "公钥已就绪，已切换认证", "公鑰已就緒，已切換驗證")
						}
						if result.Error != "" {
							if state.hostPrivacy.enabled {
								msg += " · " + t("Warning or failure; details hidden by privacy mode", "有警告或失败，隐私模式已隐藏详情", "有警告或失敗，隱私模式已隱藏詳細資料")
							} else {
								msg += " · " + state.hostKeyDeployError(result.Error)
							}
						}
					} else if !running {
						if state.hostKeyDeploy.canceled {
							msg = t("Stopped before completion", "已停止，未完成", "已停止，未完成")
						} else {
							msg = t("The server did not confirm import", "服务器未确认导入", "伺服器未確認匯入")
						}
					}
					state.declareHostKeyDeployLine(row.C, label, msg)
				})
			}).Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter).Label(title)

		})
	})
	dialog.Footer(func() {
		actions := []components.ButtonAction{{ID: hitHostKeyDeployClose, Label: t("Close", "关闭", "關閉"), Disabled: running, Variant: components.ButtonSecondary}}
		if running {
			actions = append(actions, components.ButtonAction{ID: hitHostKeyDeployCancel, Label: t("Stop remaining", "停止剩余操作", "停止剩餘操作"), Disabled: state.hostKeyDeploy.canceled, Variant: components.ButtonSecondary})
		} else if !v.started && v.confirm {
			actions = append(actions, components.ButtonAction{ID: hitHostKeyDeployBack, Label: t("Back", "上一步", "上一步"), Variant: components.ButtonSecondary},
				components.ButtonAction{ID: hitHostKeyDeployApply, Label: t("Confirm import", "确认导入", "確認匯入"), Disabled: v.changed, Variant: components.ButtonDefault})
		} else if !v.started {
			actions = append(actions, components.ButtonAction{ID: hitHostKeyDeployNext, Label: t("Review", "核对", "核對"), Disabled: !hasKey || v.changed || !validDeployTargets(v.targets), Variant: components.ButtonDefault})
		}
		state.dialogActionFooter(c, actions...)
	})
}

func (state *State) declareHostKeyDeployLine(c ui.C, title, detail string) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Text(c, title).Color(state.pal.text).Ellipsis()
		ui.Paragraph(c, detail, 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).FillWidth()
	})
}

func (state *State) hostKeyDeployError(message string) string {
	t := state.deployText
	switch message {
	case "The selected SSH key is unavailable.":
		return t(message, "所选 SSH 密钥不可用。", "所選 SSH 金鑰無法使用。")
	case "Requires an SSH host that supports non-interactive Unix commands.":
		return t(message, "该主机不支持非交互式 Unix 命令。", "該主機不支援非互動式 Unix 指令。")
	case "The connection route is unavailable or not supported for batch import.":
		return t(message, "连接路径不可用，或不支持批量导入。", "連線路徑無法使用，或不支援批次匯入。")
	case "The server did not confirm installation in ~/.ssh/authorized_keys.":
		return t(message, "服务器未确认已写入 ~/.ssh/authorized_keys。", "伺服器未確認已寫入 ~/.ssh/authorized_keys。")
	case "Public key installed. This identity has a certificate attached; automatic switching is not supported.":
		return t(message, "已写入公钥。该密钥附带证书，不支持自动切换认证。", "已寫入公鑰。該金鑰附帶憑證，不支援自動切換驗證。")
	case "Public key installed, but login settings were kept because the job was canceled.":
		return t(message, "已写入公钥。任务已取消，保持原认证设置。", "已寫入公鑰。工作已取消，保留原驗證設定。")
	case "Public key installed, but the saved host or key changed during verification. Login settings were kept.":
		return t(message, "已写入公钥。验证期间主机或密钥发生变化，保持原认证设置。", "已寫入公鑰。驗證期間主機或金鑰發生變更，保留原驗證設定。")
	case "Public key installed, but the connection route changed during verification. Login settings were kept.":
		return t(message, "已写入公钥。验证期间连接路径发生变化，保持原认证设置。", "已寫入公鑰。驗證期間連線路徑發生變更，保留原驗證設定。")
	case "Public key installed and verified, but Motty could not save the login settings.":
		return t(message, "公钥已写入并验证，但 Motty 未能保存新认证设置。", "公鑰已寫入並驗證，但 Motty 未能儲存新驗證設定。")
	}
	const prefix = "Public key installed, but key-only login was not verified: "
	if strings.HasPrefix(message, prefix) {
		return t(prefix, "已写入公钥，但仅使用新密钥登录未通过验证：", "已寫入公鑰，但僅使用新金鑰登入未通過驗證：") + strings.TrimPrefix(message, prefix)
	}
	return message
}

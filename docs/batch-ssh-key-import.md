# 批量导入 SSH 公钥 / Batch SSH public-key import

1. 在「主机」页面勾选需要操作的 SSH 主机，点击顶部「密钥管理」。
2. 选择密钥链中的一把普通私钥。Motty 只向服务器发送它的公钥。
3. 如果希望 Motty 下次用这把密钥登录，勾选「导入后切换为密钥访问」。
4. 核对主机、登录用户名和公钥指纹后确认。结果逐台显示。中途停止不会撤销已经写入的公钥或已经保存的本地配置。

仅支持已经信任主机指纹、能够执行非交互式命令并使用标准
`~/.ssh/authorized_keys` 的 Unix SSH 账号。密码、密钥口令等需已经保存；
需要临时输入验证码、仅支持交互式终端、Windows OpenSSH 或使用自定义
公钥文件的主机请单独处理。不会修改服务器的 SSH 配置、禁用密码登录、
覆盖其他公钥或移除已有公钥的限制。选择切换时，每台主机先通过独立连接
只尝试所选公钥，验证通过后才写入 Motty 的本地连接配置。
如果导入未能确认（例如断线），公钥可能已经写入；重新执行会检查重复项。

In Hosts, select the accounts to update, open **Key management**, choose a
stored ordinary SSH identity, review the account list and fingerprint, and
confirm. Turn on **Switch to key access after import** to verify the chosen
key on a fresh SSH connection before saving that host's Motty profile.
Results are reported for each host.

Batch import uses an already-trusted SSH connection to a Unix account with
saved login credentials, non-interactive commands, and the conventional
`~/.ssh/authorized_keys` location. It does not support interactive MFA,
interactive-only bastions, Windows OpenSSH, or custom key locations. It sends
no private key to the server, does not disable password login, and preserves
existing authorizations and restrictions. A lost reply can leave an import
unconfirmed; retries check for an existing key. Stopping leaves completed
remote writes and completed local profile edits intact.

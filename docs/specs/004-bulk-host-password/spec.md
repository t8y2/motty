# Bulk Host Password Replacement

Status: approved scope from the 2026-11-03 conversation.

Users can select multiple hosts and set the same new password. The action is separate from third-party import. The dialog shows the number of eligible and skipped hosts before confirmation, keeps the password masked, and never displays prior passwords. Eligible hosts are SSH with automatic/password authentication and VNC/RDP; key/agent-only SSH, Telnet and serial are skipped. A single `vault.Apply` writes all eligible host secrets atomically, without changing host metadata, authentication policy or existing sessions. Cancel, an empty password, no eligible hosts and vault failure leave all values unchanged. Success reports counts without secret text. The UI has English and Chinese labels and works in narrow layouts.

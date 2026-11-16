#!/bin/sh
# MOTTY_RDP_SECURITY_LAYER=rdp makes xrdp offer standard RDP security only, for
# TestLiveStandardSecurity. Without it xrdp negotiates TLS and NLA.
if [ "${MOTTY_RDP_SECURITY_LAYER:-}" = "rdp" ]; then
	sed -i 's/^security_layer=.*/security_layer=rdp/' /etc/xrdp/xrdp.ini
	sed -i 's/^crypt_level=.*/crypt_level=low/' /etc/xrdp/xrdp.ini
fi
rm -f /var/run/xrdp/*.pid /var/run/xrdp-sesman.pid
xrdp-sesman
exec xrdp --nodaemon

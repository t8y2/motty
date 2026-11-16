#!/usr/bin/env bash
# Manage Motty's private, persistent macOS development signing identity.
#
# The identity is deliberately separate from release certificates and from the
# application vault. It proves only that successive local development builds
# came from the same developer certificate.

MDS_FORMAT=1
MDS_IDENTIFIER=dev.motty.app.development
MDS_CONFIG_NAME=identity.conf
MDS_KEYCHAIN_NAME=development-signing.keychain-db
MDS_PASSWORD_NAME=keychain-password
MDS_LOCK_NAME=init.lock

mds_error() {
	printf 'motty development signing: %s\n' "$*" >&2
}

mds_security() {
	"${MOTTY_DEV_SIGN_SECURITY:-/usr/bin/security}" "$@"
}

mds_openssl() {
	"${MOTTY_DEV_SIGN_OPENSSL:-/usr/bin/openssl}" "$@"
}

mds_uname() {
	"${MOTTY_DEV_SIGN_UNAME:-/usr/bin/uname}" "$@"
}

mds_codesign() {
	"${MOTTY_DEV_SIGN_CODESIGN:-/usr/bin/codesign}" "$@"
}

mds_mode_owner() {
	local path=$1
	if stat -f '%Lp %u' "$path" 2>/dev/null; then {
		return 0
	}; fi
	stat -c '%a %u' "$path" 2>/dev/null
}

mds_private_path() {
	local path=$1 label=$2 want_mode=$3 stat_output mode owner
	if [ -L "$path" ]; then
		mds_error "$label is a symbolic link; manual repair is required at $MDS_SIGNING_DIR"
		return 1
	fi
	if [ ! -f "$path" ]; then
		mds_error "$label is missing; manual repair is required at $MDS_SIGNING_DIR"
		return 1
	fi
	if ! stat_output=$(mds_mode_owner "$path"); then
		mds_error "cannot inspect $label; manual repair is required at $MDS_SIGNING_DIR"
		return 1
	fi
	mode=${stat_output%% *}
	owner=${stat_output##* }
	if [ "$owner" != "$(id -u)" ] || [ "$mode" != "$want_mode" ]; then
		mds_error "$label must be owned by the current user with mode $want_mode; manual repair is required at $MDS_SIGNING_DIR"
		return 1
	fi
}

mds_prepare_directory() {
	local requested=${MOTTY_DEV_SIGNING_DIR:-}
	local default_parent current owner
	local stat_output mode
	local old_umask

	if [ -z "$requested" ]; then {
		if [ -z "${HOME:-}" ]; then
			mds_error "HOME is unset; set MOTTY_DEV_SIGNING_DIR or restore the user configuration path"
			return 1
		fi
		requested="$HOME/Library/Application Support/motty/development-signing"
	}; fi
	case "$requested" in
	/*) ;;
	*) requested="$PWD/$requested" ;;
	esac
	if [ -L "$requested" ]; then
		mds_error "$requested is a symbolic link; choose a private real directory"
		return 1
	fi
	if [ -e "$requested" ] && [ ! -d "$requested" ]; then
		mds_error "$requested exists but is not a directory"
		return 1
	fi
	if [ ! -d "$requested" ]; then
		default_parent=$(dirname "$requested")
		old_umask=$(umask)
		umask 077
		if ! mkdir -p "$default_parent"; then {
			umask "$old_umask"
			mds_error "cannot create the parent directory for $requested"
			return 1
		}; fi
		if ! mkdir "$requested"; then {
			umask "$old_umask"
			mds_error "cannot create $requested"
			return 1
		}; fi
		umask "$old_umask"
	fi
	if ! current=$(cd -P "$requested" && pwd -P); then
		mds_error "cannot resolve $requested"
		return 1
	fi
	MDS_SIGNING_DIR=$current
	MDS_CONFIG=$MDS_SIGNING_DIR/$MDS_CONFIG_NAME
	MDS_KEYCHAIN=$MDS_SIGNING_DIR/$MDS_KEYCHAIN_NAME
	MDS_PASSWORD=$MDS_SIGNING_DIR/$MDS_PASSWORD_NAME
	MDS_LOCK=$MDS_SIGNING_DIR/$MDS_LOCK_NAME

	if ! stat_output=$(mds_mode_owner "$MDS_SIGNING_DIR"); then
		mds_error "cannot inspect $MDS_SIGNING_DIR"
		return 1
	fi
	mode=${stat_output%% *}
	owner=${stat_output##* }
	if [ "$owner" != "$(id -u)" ] || [ "$mode" != 700 ]; then
		mds_error "$MDS_SIGNING_DIR must be owned by the current user with mode 700; manual repair is required"
		return 1
	fi
}

mds_check_lock() {
	if [ -L "$MDS_LOCK" ]; then
		mds_error "$MDS_LOCK is a symbolic link; manual repair is required"
		return 1
	fi
	if [ -e "$MDS_LOCK" ]; then
		mds_error "an initialization lock remains at $MDS_LOCK; inspect it and the existing material before retrying"
		return 1
	fi
}

mds_material_present() {
	if [ -e "$MDS_CONFIG" ] || [ -L "$MDS_CONFIG" ]; then {
		return 0
	}; fi
	if [ -e "$MDS_KEYCHAIN" ] || [ -L "$MDS_KEYCHAIN" ]; then {
		return 0
	}; fi
	if [ -e "$MDS_PASSWORD" ] || [ -L "$MDS_PASSWORD" ]; then {
		return 0
	}; fi
	local paths path
	paths=("$MDS_SIGNING_DIR"/.init.*)
	for path in "${paths[@]}"; do
		if [ -e "$path" ] || [ -L "$path" ]; then {
			return 0
		}; fi
	done
	return 1
}

mds_run_sensitive() {
	local label=$1 status
	shift
	local old_options
	old_options=$-
	# Bash can trace an expansion before redirection takes effect. Hiding xtrace
	# here keeps keychain passwords from command arguments out of debug logs.
	{ set +x; } 2>/dev/null
	if (umask 077; "$@") >/dev/null 2>&1; then {
		[ "${old_options#*x}" = "$old_options" ] || set -x
		return 0
	} else {
		status=$?
		[ "${old_options#*x}" = "$old_options" ] || set -x
	}; fi
	# The command may contain a keychain password in its arguments. Never repeat
	# its output, which can echo usage or trace data that includes those values.
	mds_error "$label failed with status $status; command output omitted because it may contain credentials"
	return 1
}

mds_run_without_secret_trace() {
	local old_options=$-
	local status=0
	{ set +x; } 2>/dev/null
	"$@" || status=$?
	[ "${old_options#*x}" = "$old_options" ] || set -x
	return "$status"
}

mds_random_hex() {
	local value
	if ! value=$(mds_openssl rand -hex 24 2>/dev/null) || [ -z "$value" ]; then {
		mds_error "cannot generate private keychain material"
		return 1
	}; fi
	printf '%s\n' "$value"
}

mds_write_secret() {
	local path=$1 value=$2 temporary
	local old_umask
	temporary=$path.tmp.$$
	old_umask=$(umask)
	umask 077
	if ! printf '%s\n' "$value" >"$temporary"; then {
		umask "$old_umask"
		mds_error "cannot write private keychain material"
		return 1
	}; fi
	umask "$old_umask"
	if ! chmod 600 "$temporary" || ! mv "$temporary" "$path"; then {
		mds_error "cannot publish private keychain material"
		return 1
	}; fi
}

mds_certificate_fingerprint() {
	local certificate=$1 digest=$2 expected_length=$3 label=$4 output fingerprint
	local old_options
	old_options=$-
	{ set +x; } 2>/dev/null
	if ! output=$(mds_openssl x509 -in "$certificate" -noout -fingerprint "-$digest" 2>/dev/null); then {
		[ "${old_options#*x}" = "$old_options" ] || set -x
		mds_error "cannot read the development certificate $label fingerprint"
		return 1
	}; fi
	[ "${old_options#*x}" = "$old_options" ] || set -x
	fingerprint=$(printf '%s\n' "$output" | sed -n 's/.*Fingerprint=//p' | tr -d ':[:space:]' | tr '[:lower:]' '[:upper:]')
	if [ "${#fingerprint}" -ne "$expected_length" ]; then {
		mds_error "the development certificate has no usable $label fingerprint"
		return 1
	}; fi
	printf '%s\n' "$fingerprint"
}

mds_certificate_sha1() {
	mds_certificate_fingerprint "$1" sha1 40 "SHA-1"
}

mds_certificate_sha256() {
	mds_certificate_fingerprint "$1" sha256 64 "SHA-256"
}

mds_certificate_current() {
	local certificate=$1
	if ! mds_openssl x509 -in "$certificate" -noout -checkend 0 >/dev/null 2>&1; then {
		mds_error "the development certificate is expired or not yet valid; preserve and repair the identity manually"
		return 1
	}; fi
}

mds_identity_hash() {
	local listing=$1
	local hashes count hash
	hashes=$(printf '%s\n' "$listing" | sed -n -E 's/^[[:space:]]*[0-9][0-9]*) ([0-9A-Fa-f]{40}).*/\1/p' | awk '!seen[$0]++')
	count=$(printf '%s\n' "$hashes" | awk 'NF { n++ } END { print n + 0 }')
	if [ "$count" -ne 1 ]; then {
		mds_error "the development keychain must hold exactly one code-signing identity; found $count"
		return 1
	}; fi
	hash=$(printf '%s\n' "$hashes" | awk 'NF { print; exit }' | tr '[:lower:]' '[:upper:]')
	printf '%s\n' "$hash"
}

mds_certificate_count() {
	printf '%s\n' "$1" | awk '/-----BEGIN CERTIFICATE-----/ { n++ } END { print n + 0 }'
}

mds_normalize_fingerprint() {
	printf '%s\n' "$1" | tr -d ':[:space:]' | tr '[:lower:]' '[:upper:]'
}

mds_read_config() {
	local line key value
	local format identifier keychain identity certificate
	local seen_format=0 seen_identifier=0 seen_keychain=0 seen_identity=0 seen_certificate=0

	if ! mds_private_path "$MDS_CONFIG" "development signing configuration" 600; then {
		return 1
	}; fi
	while IFS= read -r line || [ -n "$line" ]; do
		case "$line" in
		format=*)
			[ "$seen_format" -eq 0 ] || { mds_error "duplicate format in $MDS_CONFIG"; return 1; }
			seen_format=1
			format=${line#format=}
			;;
		identifier=*)
			[ "$seen_identifier" -eq 0 ] || { mds_error "duplicate identifier in $MDS_CONFIG"; return 1; }
			seen_identifier=1
			identifier=${line#identifier=}
			;;
		keychain=*)
			[ "$seen_keychain" -eq 0 ] || { mds_error "duplicate keychain in $MDS_CONFIG"; return 1; }
			seen_keychain=1
			keychain=${line#keychain=}
			;;
		identity_sha1=*)
			[ "$seen_identity" -eq 0 ] || { mds_error "duplicate identity_sha1 in $MDS_CONFIG"; return 1; }
			seen_identity=1
			identity=${line#identity_sha1=}
			;;
		certificate_sha256=*)
			[ "$seen_certificate" -eq 0 ] || { mds_error "duplicate certificate_sha256 in $MDS_CONFIG"; return 1; }
			seen_certificate=1
			certificate=${line#certificate_sha256=}
			;;
		*)
			mds_error "unrecognized line in $MDS_CONFIG; preserve it for manual repair"
			return 1
			;;
		esac
	done <"$MDS_CONFIG"
	if [ "$seen_format" -eq 0 ] || [ "$seen_identifier" -eq 0 ] || [ "$seen_keychain" -eq 0 ] ||
		[ "$seen_identity" -eq 0 ] || [ "$seen_certificate" -eq 0 ]; then {
		mds_error "the development signing configuration is incomplete; manual repair is required"
		return 1
	}; fi
	if [ "$format" != "$MDS_FORMAT" ] || [ "$identifier" != "$MDS_IDENTIFIER" ] ||
		[ "$keychain" != "$MDS_KEYCHAIN" ]; then {
		mds_error "the development signing configuration does not match this identity location; manual repair is required"
		return 1
	}; fi
	identity=$(mds_normalize_fingerprint "$identity")
	certificate=$(mds_normalize_fingerprint "$certificate")
	case "$identity" in
	????????????????????????????????????????) ;;
	*)
		mds_error "the configured identity fingerprint is invalid; manual repair is required"
		return 1
		;;
	esac
	case "$certificate" in
	????????????????????????????????????????????????????????????????) ;;
	*)
		mds_error "the configured certificate fingerprint is invalid; manual repair is required"
		return 1
		;;
	esac
	MDS_EXPECTED_IDENTITY=$identity
	MDS_EXPECTED_CERTIFICATE=$certificate
}

mds_ensure_search_list() {
	local listing line path found=0 position=0 needs_update=0
	local paths path
	paths=()
	if ! listing=$(mds_security list-keychains -d user 2>/dev/null); then {
		mds_error "cannot read the user keychain search list"
		return 1
	}; fi
	while IFS= read -r line; do
		line=$(printf '%s\n' "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
		case "$line" in
		\"*\")
			path=${line#\"}
			path=${path%\"}
			;;
		*) continue ;;
		esac
		if [ "$path" = "$MDS_KEYCHAIN" ]; then {
			if [ "$position" -ne 0 ] || [ "$found" -eq 1 ]; then {
				needs_update=1
			}; fi
			found=1
		}; fi
		if [ "$path" != "$MDS_KEYCHAIN" ]; then {
			paths[${#paths[@]}]=$path
		}; fi
		position=$((position + 1))
	done <<EOF
$listing
EOF
	if [ "$found" -eq 1 ] && [ "$needs_update" -eq 0 ]; then {
		return 0
	}; fi
	if [ "${#paths[@]}" -eq 0 ]; then {
		mds_run_sensitive "adding the development keychain to the user search list" \
			mds_security list-keychains -d user -s "$MDS_KEYCHAIN"
	} else {
		mds_run_sensitive "adding the development keychain to the user search list" \
			mds_security list-keychains -d user -s "$MDS_KEYCHAIN" "${paths[@]}"
	}; fi
}

mds_add_trust() {
	local certificate=$1 output
	if ! output=$(mds_security verify-cert -c "$certificate" -p codeSign 2>&1); then {
		if ! mds_run_sensitive "trusting the development certificate for code signing" \
			mds_security add-trusted-cert -r trustRoot -p codeSign -k "$MDS_KEYCHAIN" "$certificate"; then {
			return 1
		}; fi
	}; fi
}

mds_load_identity() {
	local password actual_identity actual_certificate actual_sha1 actual_count listing

	if ! mds_private_path "$MDS_PASSWORD" "development keychain password" 600 ||
		! mds_private_path "$MDS_KEYCHAIN" "development keychain" 600 ||
		! mds_read_config; then {
		return 1
	}; fi
	if ! IFS= read -r password <"$MDS_PASSWORD" || [ -z "$password" ]; then {
		mds_error "the development keychain password is unreadable; manual repair is required"
		return 1
	}; fi
	if ! mds_run_sensitive "unlocking the development keychain" \
		mds_security unlock-keychain -p "$password" "$MDS_KEYCHAIN"; then {
		return 1
	}; fi
	if ! listing=$(mds_security find-identity -p codesigning "$MDS_KEYCHAIN" 2>/dev/null) ||
		! actual_identity=$(mds_identity_hash "$listing"); then {
		return 1
	}; fi
	if [ "$actual_identity" != "$MDS_EXPECTED_IDENTITY" ]; then {
		mds_error "the keychain identity does not match the saved fingerprint; preserve and repair it manually"
		return 1
	}; fi
	if ! listing=$(mds_security find-certificate -a -p "$MDS_KEYCHAIN" 2>/dev/null); then {
		mds_error "cannot read the development certificate; manual repair is required"
		return 1
	}; fi
	actual_count=$(mds_certificate_count "$listing")
	if [ "$actual_count" -ne 1 ]; then {
		mds_error "the development keychain must hold exactly one certificate; found $actual_count"
		return 1
	}; fi
	actual_certificate=$(printf '%s\n' "$listing" | mds_certificate_sha256 /dev/stdin) || return 1
	if [ "$actual_certificate" != "$MDS_EXPECTED_CERTIFICATE" ]; then {
		mds_error "the keychain certificate does not match the saved fingerprint; preserve and repair it manually"
		return 1
	}; fi
	actual_sha1=$(printf '%s\n' "$listing" | mds_certificate_sha1 /dev/stdin) || return 1
	if [ "$actual_sha1" != "$MDS_EXPECTED_IDENTITY" ]; then {
		mds_error "the keychain certificate does not match the saved code-signing identity; preserve and repair it manually"
		return 1
	}; fi
	if ! printf '%s\n' "$listing" | mds_openssl x509 -noout -checkend 0 >/dev/null 2>&1; then {
		mds_error "the development certificate is expired or not yet valid; preserve and repair it manually"
		return 1
	}; fi
	if ! mds_ensure_search_list; then {
		return 1
	}; fi
	MDS_IDENTITY=$MDS_EXPECTED_IDENTITY
	MDS_CERTIFICATE=$MDS_EXPECTED_CERTIFICATE
	MDS_CERTIFICATE_SHA1=$actual_sha1
}

mds_clear_failed_initialization() {
	local work=${1:-} status=0 path
	for path in "$MDS_CONFIG" "$MDS_PASSWORD" "$MDS_KEYCHAIN" \
		"$MDS_CONFIG.tmp.$$" "$MDS_PASSWORD.tmp.$$"; do
		if [ -e "$path" ] || [ -L "$path" ]; then {
			rm -f "$path" || status=1
		}; fi
	done
	if [ -n "$work" ]; then {
		for path in "$work/key.pem" "$work/certificate.pem" "$work/identity.p12" "$work/openssl.cnf"; do
			if [ -e "$path" ] || [ -L "$path" ]; then {
				rm -f "$path" || status=1
			}; fi
		done
		if [ -d "$work" ]; then {
			rmdir "$work" 2>/dev/null || status=1
		}; fi
	}; fi
	if [ -d "$MDS_LOCK" ]; then {
		rmdir "$MDS_LOCK" 2>/dev/null || status=1
	}; fi
	return "$status"
}

mds_initialize_identity_material() {
	local work=$1
	local keychain_password p12_password key certificate p12 config_text
	local identity fingerprint certificate_sha1 old_umask

	key=$work/key.pem
	certificate=$work/certificate.pem
	p12=$work/identity.p12
	if ! keychain_password=$(mds_random_hex) || ! p12_password=$(mds_random_hex); then {
		return 1
	}; fi
	if ! mds_write_secret "$MDS_PASSWORD" "$keychain_password"; then {
		return 1
	}; fi
	old_umask=$(umask)
	umask 077
	if ! cat >"$work/openssl.cnf" <<'EOF'
[req]
prompt = no
distinguished_name = subject
x509_extensions = signing

[subject]
CN = Motty Development Signing
O = Motty Development

[signing]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = codeSigning
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
EOF
	then {
		umask "$old_umask"
		mds_error "cannot write the private certificate configuration"
		return 1
	}; fi
	umask "$old_umask"
	if ! mds_run_sensitive "generating the development certificate" \
		mds_openssl req -new -newkey rsa:2048 -nodes -x509 -sha256 \
		-days 3650 -config "$work/openssl.cnf" -keyout "$key" -out "$certificate"; then {
		return 1
	}; fi
	if ! mds_run_sensitive "packaging the development identity" \
		mds_openssl pkcs12 -export -inkey "$key" -in "$certificate" \
		-out "$p12" -name "Motty Development Signing" -passout "pass:$p12_password"; then {
		return 1
	}; fi
	if ! mds_run_sensitive "creating the private development keychain" \
		mds_security create-keychain -p "$keychain_password" "$MDS_KEYCHAIN" ||
		! mds_run_sensitive "setting development keychain lock policy" \
			mds_security set-keychain-settings -lut 21600 "$MDS_KEYCHAIN" ||
		! mds_run_sensitive "unlocking the private development keychain" \
			mds_security unlock-keychain -p "$keychain_password" "$MDS_KEYCHAIN" ||
		! mds_run_sensitive "importing the development identity" \
			mds_security import "$p12" -k "$MDS_KEYCHAIN" -P "$p12_password" \
			-T /usr/bin/codesign -T /usr/bin/security ||
		! mds_run_sensitive "allowing codesign to use the development key" \
			mds_security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
			-k "$keychain_password" "$MDS_KEYCHAIN"; then {
		return 1
	}; fi
	if ! chmod 600 "$MDS_KEYCHAIN"; then {
		mds_error "cannot restrict access to the development keychain"
		return 1
	}; fi
	if ! listing=$(mds_security find-identity -p codesigning "$MDS_KEYCHAIN" 2>/dev/null) ||
		! identity=$(mds_identity_hash "$listing") ||
		! fingerprint=$(mds_certificate_sha256 "$certificate") ||
		! certificate_sha1=$(mds_certificate_sha1 "$certificate") ||
		! mds_certificate_current "$certificate"; then {
		return 1
	}; fi
	if [ "$identity" != "$certificate_sha1" ]; then {
		mds_error "the development keychain identity does not match its certificate; preserve the material for manual repair"
		return 1
	}; fi
	config_text=$(printf 'format=%s\nidentifier=%s\nkeychain=%s\nidentity_sha1=%s\ncertificate_sha256=%s\n' \
		"$MDS_FORMAT" "$MDS_IDENTIFIER" "$MDS_KEYCHAIN" "$identity" "$fingerprint")
	if ! mds_add_trust "$certificate"; then {
		return 1
	}; fi
	if ! mds_write_secret "$MDS_CONFIG" "$config_text"; then {
		return 1
	}; fi
}

mds_initialize_identity() {
	local old_umask work

	old_umask=$(umask)
	umask 077
	if ! mkdir "$MDS_LOCK"; then {
		umask "$old_umask"
		mds_error "cannot take the initialization lock at $MDS_LOCK; another initializer may be running"
		return 1
	}; fi
	umask "$old_umask"
	if ! work=$(mktemp -d "$MDS_SIGNING_DIR/.init.XXXXXX"); then {
		mds_clear_failed_initialization || mds_error "failed initialization left temporary files at $MDS_SIGNING_DIR; repair manually"
		mds_error "cannot create a private initialization directory at $MDS_SIGNING_DIR"
		return 1
	}; fi
	if ! mds_initialize_identity_material "$work"; then {
		mds_clear_failed_initialization "$work" ||
			mds_error "failed initialization left temporary files at $MDS_SIGNING_DIR; repair manually"
		return 1
	}; fi
	if ! rm -f "$work/key.pem" "$work/certificate.pem" "$work/identity.p12" "$work/openssl.cnf" ||
		! rmdir "$work"; then {
		mds_error "the identity was created but private initialization files could not be removed; repair manually"
		return 1
	}; fi
	if ! rmdir "$MDS_LOCK"; then {
		mds_error "the identity was created but its initialization lock could not be removed"
		return 1
	}; fi
	if ! mds_load_identity; then {
		return 1
	}; fi
}

macos_dev_signing_ensure_default_impl() {
	local operating_system
	if ! operating_system=$(mds_uname -s 2>/dev/null); then {
		mds_error "cannot determine the operating system"
		return 1
	}; fi
	if [ "$operating_system" != Darwin ]; then {
		mds_error "the persistent development identity is only available on macOS"
		return 1
	}; fi
	if ! mds_prepare_directory; then {
		return 1
	}; fi
	if ! mds_check_lock; then {
		return 1
	}; fi
	if ! mds_material_present; then {
		if ! mds_initialize_identity; then {
			return 1
		}; fi
		return 0
	}; fi
	if [ ! -e "$MDS_CONFIG" ]; then {
		mds_error "development signing material is incomplete; preserve it for manual repair at $MDS_SIGNING_DIR"
		return 1
	}; fi
	if mds_load_identity; then {
		return 0
	}; fi
	return 1
}

macos_dev_signing_ensure_default() {
	mds_run_without_secret_trace macos_dev_signing_ensure_default_impl
}

macos_dev_signing_verify() {
	local binary=$1 requirement
	if [ ! -f "$binary" ]; then
		mds_error "cannot sign $binary: the executable does not exist"
		return 1
	fi
	# codesign echoes the signed path but not key material. The requirement
	# explicitly binds both the development identifier and this certificate.
	requirement=$(printf 'identifier "%s" and certificate leaf = H"%s"' "$MDS_IDENTIFIER" "$MDS_CERTIFICATE_SHA1")
	if ! mds_run_sensitive "signing the development executable" \
		mds_codesign --force --sign "$MDS_IDENTITY" --keychain "$MDS_KEYCHAIN" \
		--identifier "$MDS_IDENTIFIER" --requirements "=designated => $requirement" \
		--timestamp=none "$binary"; then {
		return 1
	}; fi
	if ! mds_run_sensitive "verifying the development signature" \
		mds_codesign --verify --strict -R="$requirement" "$binary"; then {
		return 1
	}; fi
}

macos_dev_signing_sign_none() {
	local binary=$1
	if [ ! -f "$binary" ]; then
		mds_error "cannot check $binary: the executable does not exist"
		return 1
	fi
	mds_error "warning: signing disabled; macOS keychain authorization may repeat after a rebuild"
}

mds_custom_identity_sha1() {
	local identity=$1 listing wanted hashes hash count
	wanted=$(mds_normalize_fingerprint "$identity")
	if ! listing=$(mds_security find-identity -v -p codesigning 2>/dev/null); then {
		mds_error "cannot read the code-signing identities available to the requested certificate override"
		return 1
	}; fi
	hashes=$(
		printf '%s\n' "$listing" |
			sed -n -E 's/^[[:space:]]*[0-9]+\) ([0-9A-Fa-f]{40}) "(.*)"$/\1\t\2/p' |
			while IFS=$'\t' read -r hash name; do
				hash=$(mds_normalize_fingerprint "$hash")
				if [ "$hash" = "$wanted" ] || [ "$name" = "$identity" ]; then {
					printf '%s\n' "$hash"
				}; fi
			done |
			awk '!seen[$0]++'
	)
	count=$(printf '%s\n' "$hashes" | awk 'NF { n++ } END { print n + 0 }')
	if [ "$count" -ne 1 ]; then {
		mds_error "the requested signing identity must name exactly one valid code-signing certificate; found $count"
		return 1
	}; fi
	printf '%s\n' "$hashes"
}

macos_dev_signing_sign_custom() {
	local binary=$1 identity=$2 requirement certificate_sha1
	if [ ! -f "$binary" ]; then
		mds_error "cannot sign $binary: the executable does not exist"
		return 1
	fi
	if ! certificate_sha1=$(mds_custom_identity_sha1 "$identity"); then {
		return 1
	}; fi
	requirement=$(printf 'identifier "%s" and certificate leaf = H"%s"' "$MDS_IDENTIFIER" "$certificate_sha1")
	if ! mds_run_sensitive "signing the development executable with the requested identity" \
		mds_codesign --force --sign "$identity" --identifier "$MDS_IDENTIFIER" \
		--requirements "=designated => $requirement" \
		--timestamp=none "$binary"; then {
		return 1
	}; fi
	if ! mds_run_sensitive "verifying the requested development signature" \
		mds_codesign --verify --strict -R="$requirement" "$binary"; then {
		return 1
	}; fi
}

macos_dev_signing_sign_impl() {
	local binary=$1 identity=${2:-}
	local operating_system
	if ! operating_system=$(mds_uname -s 2>/dev/null); then {
		mds_error "cannot determine the operating system"
		return 1
	}; fi
	if [ "$operating_system" != Darwin ]; then {
		# Other supported platforms build and launch without macOS signing.
		return 0
	}; fi
	case "$identity" in
	none)
		macos_dev_signing_sign_none "$binary"
		;;
	"")
		if ! macos_dev_signing_ensure_default; then {
			return 1
		}; fi
		macos_dev_signing_verify "$binary"
		;;
	*)
		macos_dev_signing_sign_custom "$binary" "$identity"
		;;
	esac
}

macos_dev_signing_sign() {
	mds_run_without_secret_trace macos_dev_signing_sign_impl "$@"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	set -eu
	command_name=${1:-}
	case "$command_name" in
	load)
		macos_dev_signing_ensure_default
		printf 'keychain=%s\nidentity_sha1=%s\ncertificate_sha256=%s\n' \
			"$MDS_KEYCHAIN" "$MDS_IDENTITY" "$MDS_CERTIFICATE"
		;;
	sign)
		if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
			mds_error "usage: $0 sign BINARY [IDENTITY|none]"
			exit 2
		fi
		macos_dev_signing_sign "$2" "${3:-}"
		;;
	*)
		mds_error "usage: $0 load | sign BINARY [IDENTITY|none]"
		exit 2
		;;
	esac
fi

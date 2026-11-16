package sshconn

// This runs before the cwd encoder temporarily selects LC_ALL=C. It inspects
// the actual interactive environment after user profiles, not an exec channel.
const inputEnvironmentFunction = `__motty_input_report() {
    [[ -n '__MOTTY_INPUT_TOKEN__' ]] || return 0
    local _motty_input_now="${LC_ALL-}|${LC_CTYPE-}|${LANG-}"
    local _motty_input_map _motty_input_try _motty_input_count
    if [[ ${_motty_input_key-} != "$_motty_input_now" || -z ${_motty_input_charset-} ]]; then
        _motty_input_key=$_motty_input_now
        _motty_input_charset=unknown
        _motty_input_locale=
        if command -v locale >/dev/null 2>&1; then
            _motty_input_map=$(LC_ALL="${LC_ALL-}" LC_CTYPE="${LC_CTYPE-}" LANG="${LANG-}" command locale charmap 2>/dev/null) || _motty_input_map=
            case $_motty_input_map in
                UTF-8|UTF8|utf-8|utf8) _motty_input_charset=utf8 ;;
                '') ;;
                *) _motty_input_charset=other ;;
            esac
            if [[ $_motty_input_charset != utf8 ]]; then
                _motty_input_count=0
                for _motty_input_try in C.UTF-8 C.utf8 en_US.UTF-8 en_US.utf8 $(command locale -a 2>/dev/null); do
                    (( ++_motty_input_count ))
                    (( _motty_input_count <= 64 )) || break
                    case $_motty_input_try in
                        ''|*[!a-zA-Z0-9_.@-]*) continue ;;
                    esac
                    (( ${#_motty_input_try} <= 128 )) || continue
                    _motty_input_map=$(LC_ALL="$_motty_input_try" command locale charmap 2>/dev/null) || continue
                    case $_motty_input_map in
                        UTF-8|UTF8|utf-8|utf8) _motty_input_locale=$_motty_input_try; break ;;
                    esac
                done
            fi
        fi
    fi
    printf '\033]777;motty-input;__MOTTY_INPUT_TOKEN__;%s;%s\007' "$_motty_input_charset" "$_motty_input_locale"
}
`

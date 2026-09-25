#!/bin/sh
# Source this installer from Bash/Zsh for immediate use, or run it with sh
# and open a new terminal. All work except the final PATH update is isolated.
if (
    set -u
    fail() { printf 'xmind-md: %s\n' "$*" >&2; exit 1; }
    [ -n "${HOME:-}" ] || fail 'HOME is not set.'
    case "$HOME" in /*) ;; *) fail 'HOME must be an absolute path.' ;; esac
    case "$HOME" in *:*) fail 'HOME cannot contain a colon in a PATH entry.' ;; esac

    os=$(uname -s) || fail 'Cannot detect the operating system.'
    case "$os" in Darwin) os=darwin ;; Linux) os=linux ;; *) fail "Unsupported operating system: $os" ;; esac
    arch=$(uname -m) || fail 'Cannot detect the processor architecture.'
    # Prefer a native Apple Silicon executable when launched under Rosetta.
    if [ "$os" = darwin ] && [ "$(sysctl -in sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
        arch=arm64
    fi
    case "$arch" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail "Unsupported processor architecture: $arch" ;; esac

    shell_name=${SHELL:-sh}
    shell_name=${shell_name##*/}
    case "$shell_name" in
        bash|zsh|fish|sh|dash|ksh|'') ;;
        *) fail "Automatic PATH setup supports Bash, Zsh, Fish, and POSIX shells; found $shell_name." ;;
    esac
    version=${XMIND_MD_VERSION:-latest}
    base=https://github.com/michaelmjhhhh/mark-Xmind-down/releases
    if [ "$version" = latest ]; then
        base=$base/latest/download
    else
        case "$version" in v[0-9]*) ;; *) fail 'XMIND_MD_VERSION must be a release tag such as v0.1.0.' ;; esac
        case "$version" in *[!A-Za-z0-9._-]*) fail 'Invalid release tag.' ;; esac
        base=$base/download/$version
    fi
    command -v curl >/dev/null 2>&1 || fail 'curl is required.'
    if command -v sha256sum >/dev/null 2>&1; then
        checksum() { sha256sum "$1"; }
    elif command -v shasum >/dev/null 2>&1; then
        checksum() { shasum -a 256 "$1"; }
    else
        fail 'A SHA-256 tool (sha256sum or shasum) is required.'
    fi

    work=$(mktemp -d) || fail 'Cannot create a temporary directory.'
    stage=''
    trap 'rm -rf "$work"; [ -z "$stage" ] || rm -f "$stage"' 0
    trap 'exit 1' HUP INT TERM
    asset=xmind-md-$os-$arch
    printf 'Downloading xmind-md (%s/%s, %s)…\n' "$os" "$arch" "$version"
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/$asset" -o "$work/$asset" || fail 'Binary download failed; installation was not changed.'
    [ -s "$work/$asset" ] || fail 'The downloaded executable is empty.'
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/SHA256SUMS" -o "$work/SHA256SUMS" || fail 'Checksum download failed; installation was not changed.'
    expected=$(awk -v asset="$asset" '$2 == asset { hash=$1; count++ } END { if (count != 1) exit 1; print hash }' "$work/SHA256SUMS") || fail 'The checksum manifest has no unique entry for this platform.'
    case "$expected" in *[!0-9a-fA-F]*|'') fail 'Invalid SHA-256 checksum.' ;; esac
    [ "${#expected}" -eq 64 ] || fail 'Invalid SHA-256 checksum length.'
    expected=$(printf '%s' "$expected" | tr 'A-F' 'a-f')
    actual=$(checksum "$work/$asset" | awk '{print $1}')
    [ "$actual" = "$expected" ] || fail 'Checksum verification failed; installation was not changed.'

    # Append only our own idempotent block; preserve the user's existing config.
    configure_posix() {
        profile=$1
        [ ! -d "$profile" ] || fail "Expected a shell config file, found directory: $profile"
        if [ -f "$profile" ] && grep -Fq '# >>> xmind-md PATH >>>' "$profile"; then
            grep -Fq '# <<< xmind-md PATH <<<' "$profile" || fail "Incomplete xmind-md PATH block in $profile"
            return
        fi
        mkdir -p "$(dirname "$profile")" || fail "Cannot create the directory for $profile"
        cat >> "$profile" <<'PROFILE' || fail "Cannot update $profile"

# >>> xmind-md PATH >>>
PATH="$(
    remaining=${PATH-}; result=$HOME/.local/bin; last=0
    while [ "$last" = 0 ]; do
        case "$remaining" in
            (*:*) entry=${remaining%%:*}; remaining=${remaining#*:} ;;
            (*) entry=$remaining; last=1 ;;
        esac
        [ "$entry" = "$HOME/.local/bin" ] || result=$result:$entry
    done
    printf '%s' "$result"
)"
export PATH
# <<< xmind-md PATH <<<
PROFILE
    }
    case "$shell_name" in
        zsh)
            configure_posix "${ZDOTDIR:-$HOME}/.zprofile"
            configure_posix "${ZDOTDIR:-$HOME}/.zshrc"
            ;;
        bash)
            configure_posix "$HOME/.bashrc"
            if [ -f "$HOME/.bash_profile" ]; then
                configure_posix "$HOME/.bash_profile"
            elif [ -f "$HOME/.bash_login" ]; then
                configure_posix "$HOME/.bash_login"
            else
                configure_posix "$HOME/.profile"
            fi
            ;;
        fish)
            profile=${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/xmind-md.fish
            mkdir -p "$(dirname "$profile")" || fail 'Cannot create the Fish configuration directory.'
            if [ ! -f "$profile" ] || ! grep -Fq '# >>> xmind-md PATH >>>' "$profile"; then
                cat >> "$profile" <<'FISH' || fail 'Cannot update Fish PATH configuration.'

# >>> xmind-md PATH >>>
begin
    set -l xmind_remaining
    for entry in $PATH
        if test "$entry" != "$HOME/.local/bin"
            set -a xmind_remaining "$entry"
        end
    end
    set -gx PATH "$HOME/.local/bin" $xmind_remaining
end
# <<< xmind-md PATH <<<
FISH
            fi
            ;;
        *) configure_posix "$HOME/.profile" ;;
    esac

    install_dir=$HOME/.local/bin
    mkdir -p "$install_dir" || fail "Cannot create $install_dir"
    [ ! -d "$install_dir/xmind-md" ] || fail 'The installation target is a directory.'
    stage=$(mktemp "$install_dir/.xmind-md.XXXXXX") || fail 'Cannot stage the executable.'
    cp "$work/$asset" "$stage" && chmod 755 "$stage" || fail 'Cannot prepare the executable.'
    mv -f "$stage" "$install_dir/xmind-md" || fail 'Cannot install the executable.'
    stage=''
    printf 'Installed %s/xmind-md\nPATH configured automatically.\n' "$install_dir"
); then
    # When sourced, this updates the calling shell without changing its options,
    # positional arguments, traps, working directory, or temporary variables.
    PATH="$(
        remaining=${PATH-}; result=$HOME/.local/bin; last=0
        while [ "$last" = 0 ]; do
            case "$remaining" in
                (*:*) entry=${remaining%%:*}; remaining=${remaining#*:} ;;
                (*) entry=$remaining; last=1 ;;
            esac
            [ "$entry" = "$HOME/.local/bin" ] || result=$result:$entry
        done
        printf '%s' "$result"
    )"
    export PATH
    hash -r 2>/dev/null || true
else
    # Nonzero status without exiting the user's interactive shell when sourced.
    (exit 1)
fi

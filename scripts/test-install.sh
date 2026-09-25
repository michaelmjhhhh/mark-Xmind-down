#!/bin/sh
# Isolated installer integration tests; no network or real user files modified.
set -eu
root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' 0
trap 'exit 1' HUP INT TERM
mkdir -p "$test_dir/tools" "$test_dir/downloads"
cat > "$test_dir/downloads/binary" <<'BINARY'
#!/bin/sh
printf 'xmind-md fixture\n'
BINARY
if command -v sha256sum >/dev/null 2>&1; then
    digest=$(sha256sum "$test_dir/downloads/binary" | awk '{print $1}')
else
    digest=$(shasum -a 256 "$test_dir/downloads/binary" | awk '{print $1}')
fi
for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
    printf '%s  xmind-md-%s\n' "$digest" "$platform" >> "$test_dir/downloads/SHA256SUMS"
done
cat > "$test_dir/tools/curl" <<'CURL'
#!/bin/sh
url='' dest=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) shift; dest=$1 ;;
        https://*) url=$1 ;;
    esac
    shift
done
printf '%s\n' "$url" >> "$XMIND_TEST_DOWNLOADS/requests"
[ "${XMIND_TEST_FAIL:-0}" != 1 ] || exit 22
case "$url" in
    */SHA256SUMS) cp "$XMIND_TEST_DOWNLOADS/SHA256SUMS" "$dest" ;;
    *) cp "$XMIND_TEST_DOWNLOADS/binary" "$dest"
       if [ "${XMIND_TEST_CORRUPT:-0}" = 1 ]; then printf 'corrupt' >> "$dest"; fi ;;
esac
CURL
cat > "$test_dir/tools/uname" <<'UNAME'
#!/bin/sh
case "$1" in -s) printf '%s\n' "${XMIND_TEST_OS:-Linux}" ;; -m) printf '%s\n' "${XMIND_TEST_ARCH:-x86_64}" ;; esac
UNAME
cat > "$test_dir/tools/sysctl" <<'SYSCTL'
#!/bin/sh
printf '%s\n' "${XMIND_TEST_ROSETTA:-0}"
SYSCTL
chmod +x "$test_dir/tools/"*
test_path=$test_dir/tools:$PATH
export XMIND_TEST_DOWNLOADS="$test_dir/downloads"

run_installer() {
    env HOME="$test_dir/user home" SHELL=/bin/bash PATH="$test_path" "$@"
}
mkdir -p "$test_dir/user home"
printf '# existing custom configuration\n' > "$test_dir/user home/.bash_profile"
printf '# existing interactive configuration\n' > "$test_dir/user home/.bashrc"

# Sourcing installs and updates the SAME process, without altering shell flags,
# positional arguments or the working directory. Spaces in HOME are deliberate.
env HOME="$test_dir/user home" SHELL=/bin/bash PATH="$test_path" INSTALL_SCRIPT="$root/scripts/install.sh" bash -c '
    set -- "preserved argument"
    before_flags=$-; before_dir=$PWD
    . "$INSTALL_SCRIPT" || exit 1
    test "$-" = "$before_flags" && test "$PWD" = "$before_dir" && test "$1" = "preserved argument" || exit 1
    test "$(command -v xmind-md)" = "$HOME/.local/bin/xmind-md" || exit 1
    test "$(xmind-md --version)" = "xmind-md fixture" || exit 1
    . "$INSTALL_SCRIPT" || exit 1
    test "$(printf "%s" "$PATH" | tr : "\n" | grep -Fxc "$HOME/.local/bin")" = 1
'
for profile in .bashrc .bash_profile; do
    test "$(grep -Fc '# >>> xmind-md PATH >>>' "$test_dir/user home/$profile")" = 1
    grep -q '^# existing' "$test_dir/user home/$profile"
done
# A fresh shell starting with the OLD PATH picks up the saved configuration.
run_installer bash --noprofile --rcfile "$test_dir/user home/.bashrc" -ic 'test "$(xmind-md --version)" = "xmind-md fixture"' </dev/null

# An older Go installation must not shadow the newly installed executable,
# even when .local/bin already appears later (or twice) in PATH.
mkdir -p "$test_dir/old Go bin"
printf '#!/bin/sh\nprintf "old version\\n"\n' > "$test_dir/old Go bin/xmind-md"
chmod +x "$test_dir/old Go bin/xmind-md"
shadow_path="$test_dir/old Go bin:$test_path:$test_dir/user home/.local/bin:$test_dir/user home/.local/bin"
env HOME="$test_dir/user home" SHELL=/bin/bash PATH="$shadow_path" INSTALL_SCRIPT="$root/scripts/install.sh" bash -c '
    . "$INSTALL_SCRIPT" || exit 1
    test "$(xmind-md --version)" = "xmind-md fixture" || exit 1
    test "$(printf "%s" "$PATH" | tr : "\n" | grep -Fxc "$HOME/.local/bin")" = 1
'
env HOME="$test_dir/user home" PATH="$shadow_path" bash --noprofile --rcfile "$test_dir/user home/.bashrc" -ic 'test "$(xmind-md --version)" = "xmind-md fixture"' </dev/null

cp "$test_dir/user home/.local/bin/xmind-md" "$test_dir/before"
cp "$test_dir/user home/.bashrc" "$test_dir/profile-before"
for failure in XMIND_TEST_CORRUPT=1 XMIND_TEST_FAIL=1 XMIND_TEST_ARCH=riscv64 XMIND_MD_VERSION=../bad; do
    if run_installer env "$failure" sh "$root/scripts/install.sh" > "$test_dir/error" 2>&1; then
        printf 'Expected failure for %s\n' "$failure" >&2; exit 1
    fi
    cmp "$test_dir/before" "$test_dir/user home/.local/bin/xmind-md"
    cmp "$test_dir/profile-before" "$test_dir/user home/.bashrc"
done

# OS/architecture selection and version pins must request the exact release URL.
run_installer env XMIND_TEST_OS=Darwin XMIND_TEST_ARCH=x86_64 XMIND_TEST_ROSETTA=1 XMIND_MD_VERSION=v0.1.0 sh "$root/scripts/install.sh"
grep -q '/releases/download/v0.1.0/xmind-md-darwin-arm64$' "$test_dir/downloads/requests"
run_installer env XMIND_TEST_ARCH=aarch64 sh "$root/scripts/install.sh"
grep -q '/releases/latest/download/xmind-md-linux-arm64$' "$test_dir/downloads/requests"

# Zsh honors custom ZDOTDIR and works immediately when sourced, too.
if command -v zsh >/dev/null 2>&1; then
    env HOME="$test_dir/zsh home" ZDOTDIR="$test_dir/zsh config" SHELL=/bin/zsh PATH="$test_path" INSTALL_SCRIPT="$root/scripts/install.sh" zsh -c '. "$INSTALL_SCRIPT" && xmind-md --version'
    env HOME="$test_dir/zsh home" ZDOTDIR="$test_dir/zsh config" SHELL=/bin/zsh PATH="$test_path" zsh -ic 'test "$(xmind-md --version)" = "xmind-md fixture"' </dev/null
    test -f "$test_dir/zsh config/.zprofile"
fi

env HOME="$test_dir/fish home" XDG_CONFIG_HOME="$test_dir/fish config" SHELL=/usr/bin/fish PATH="$test_path" sh "$root/scripts/install.sh"
test -f "$test_dir/fish config/fish/conf.d/xmind-md.fish"
if command -v fish >/dev/null 2>&1; then
    env HOME="$test_dir/fish home" XDG_CONFIG_HOME="$test_dir/fish config" PATH="$test_path" fish -c 'test (xmind-md --version) = "xmind-md fixture"'
fi
env HOME="$test_dir/posix home" SHELL=/bin/sh PATH="$test_path" sh "$root/scripts/install.sh"
env HOME="$test_dir/posix home" PATH="$test_path" sh -c '. "$HOME/.profile"; test "$(xmind-md --version)" = "xmind-md fixture"'
printf 'Installer tests passed (current shell, new shells, repeat install, checksums, failures, platforms).\n'

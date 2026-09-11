# wate shell integration for fish — add to ~/.config/fish/config.fish:  source ~/.config/wate/shell/wate.fish
status is-interactive; or exit 0
set -q WATE_PANE_ID; or exit 0
function _wate_osc7 --on-variable PWD
    printf '\e]7;file://%s%s\e\\' (hostname) "$PWD"
end
function _wate_prompt --on-event fish_prompt
    _wate_osc7
    printf '\e]133;A\e\\'
end
function _wate_preexec --on-event fish_preexec
    printf '\e]133;C\e\\'
end
function _wate_postexec --on-event fish_postexec
    printf '\e]133;D;%s\e\\' $status
end

# Word-wise motion and deletion, per [terminal] word_keys (see WATE_WORD_KEYS).
set -l _wate_mode (test -n "$WATE_WORD_KEYS"; and echo $WATE_WORD_KEYS; or echo ctrl)
if test "$_wate_mode" = ctrl -o "$_wate_mode" = both
    bind \e\[1\;5D backward-word
    bind \e\[1\;5C forward-word
    bind \e\[3\;5~ kill-word
    bind \b backward-kill-word
end
if test "$_wate_mode" = alt -o "$_wate_mode" = both
    bind \e\[1\;3D backward-word
    bind \e\[1\;3C forward-word
    bind \e\[3\;3~ kill-word
    bind \e\x7f backward-kill-word
end

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

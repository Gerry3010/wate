# wate shell integration for bash — add to ~/.bashrc:  source ~/.config/wate/shell/wate.bash
[[ $- == *i* && -n $WATE_PANE_ID ]] || return 0
_wate_osc7() { printf '\e]7;file://%s%s\e\\' "${HOSTNAME:-localhost}" "$PWD"; }
_wate_prompt() { local ret=$?; [[ -n $_wate_cmd_started ]] && printf '\e]133;D;%s\e\\' "$ret"; _wate_cmd_started=; _wate_osc7; printf '\e]133;A\e\\'; }
_wate_preexec() { [[ -n $COMP_LINE || -n $_wate_cmd_started ]] && return; _wate_cmd_started=1; printf '\e]133;C\e\\'; }
PROMPT_COMMAND="_wate_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
trap '_wate_preexec' DEBUG

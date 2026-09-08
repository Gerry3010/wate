# yate shell integration for bash — add to ~/.bashrc:  source ~/.config/yate/shell/yate.bash
[[ $- == *i* && -n $YATE_PANE_ID ]] || return 0
_yate_osc7() { printf '\e]7;file://%s%s\e\\' "${HOSTNAME:-localhost}" "$PWD"; }
_yate_prompt() { local ret=$?; [[ -n $_yate_cmd_started ]] && printf '\e]133;D;%s\e\\' "$ret"; _yate_cmd_started=; _yate_osc7; printf '\e]133;A\e\\'; }
_yate_preexec() { [[ -n $COMP_LINE || -n $_yate_cmd_started ]] && return; _yate_cmd_started=1; printf '\e]133;C\e\\'; }
PROMPT_COMMAND="_yate_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
trap '_yate_preexec' DEBUG

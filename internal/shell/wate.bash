# wate shell integration for bash — add to ~/.bashrc:  source ~/.config/wate/shell/wate.bash
[[ $- == *i* && -n $WATE_PANE_ID ]] || return 0
_wate_osc7() { printf '\e]7;file://%s%s\e\\' "${HOSTNAME:-localhost}" "$PWD"; }
_wate_prompt() { local ret=$?; [[ -n $_wate_cmd_started ]] && printf '\e]133;D;%s\e\\' "$ret"; _wate_cmd_started=; _wate_osc7; printf '\e]133;A\e\\'; }
_wate_preexec() { [[ -n $COMP_LINE || -n $_wate_cmd_started ]] && return; _wate_cmd_started=1; printf '\e]133;C\e\\'; }
PROMPT_COMMAND="_wate_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
trap '_wate_preexec' DEBUG

# Word-wise motion and deletion, per [terminal] word_keys (see WATE_WORD_KEYS).
_wate_word_keys() {
  local mode=${WATE_WORD_KEYS:-ctrl}
  [[ $mode == off ]] && return 0
  if [[ $mode == ctrl || $mode == both ]]; then
    bind '"\e[1;5D": backward-word' '"\e[1;5C": forward-word' '"\e[3;5~": kill-word' '"\C-h": backward-kill-word'
  fi
  if [[ $mode == alt || $mode == both ]]; then
    bind '"\e[1;3D": backward-word' '"\e[1;3C": forward-word' '"\e[3;3~": kill-word' '"\e\C-?": backward-kill-word'
  fi
}
_wate_word_keys

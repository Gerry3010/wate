# yate shell integration for zsh — sourced automatically via ZDOTDIR.
# Emits OSC 7 (cwd) and OSC 133 prompt marks so yate can inherit the working
# directory into new panes and resize without prompt artifacts.
[[ -o interactive ]] || return 0
[[ -n $YATE_PANE_ID ]] || return 0

_yate_osc7() { printf '\e]7;file://%s%s\e\\' "${HOST:-localhost}" "${PWD}" }
_yate_precmd() {
  local ret=$?
  [[ -n $_yate_cmd_started ]] && printf '\e]133;D;%s\e\\' "$ret"
  _yate_cmd_started=
  _yate_osc7
  # Prompt start mark: precmd runs right before the prompt is drawn, so this lands
  # exactly where the prompt begins — regardless of the prompt framework in use.
  printf '\e]133;A\e\\'
}
_yate_preexec() {
  _yate_cmd_started=1
  printf '\e]133;C\e\\'
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd _yate_precmd
add-zsh-hook preexec _yate_preexec
add-zsh-hook chpwd _yate_osc7

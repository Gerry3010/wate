# wate shell integration for zsh — sourced automatically via ZDOTDIR.
# Emits OSC 7 (cwd) and OSC 133 prompt marks so wate can inherit the working
# directory into new panes and resize without prompt artifacts.
[[ -o interactive ]] || return 0
[[ -n $WATE_PANE_ID ]] || return 0

_wate_osc7() { printf '\e]7;file://%s%s\e\\' "${HOST:-localhost}" "${PWD}" }
_wate_precmd() {
  local ret=$?
  [[ -n $_wate_cmd_started ]] && printf '\e]133;D;%s\e\\' "$ret"
  _wate_cmd_started=
  _wate_osc7
  # Prompt start mark: precmd runs right before the prompt is drawn, so this lands
  # exactly where the prompt begins — regardless of the prompt framework in use.
  printf '\e]133;A\e\\'
}
_wate_preexec() {
  _wate_cmd_started=1
  printf '\e]133;C\e\\'
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd _wate_precmd
add-zsh-hook preexec _wate_preexec
add-zsh-hook chpwd _wate_osc7

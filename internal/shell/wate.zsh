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

# Word-wise motion and deletion (see [terminal] word_keys). Bound from the first precmd, i.e.
# after the user's .zshrc: oh-my-zsh and friends rebind these keys while they load, so binding
# at source time (this file runs before .zshrc) would lose the race.
_wate_bind_words() {
  add-zsh-hook -d precmd _wate_bind_words
  local mode=${WATE_WORD_KEYS:-ctrl}
  [[ $mode == off ]] && return 0
  local -a maps=(${(f)"$(bindkey -l)"})
  local m
  for m in emacs viins vicmd; do
    (( ${maps[(I)$m]} )) || continue
    if [[ $mode == ctrl || $mode == both ]]; then
      bindkey -M $m '^[[1;5D' backward-word
      bindkey -M $m '^[[1;5C' forward-word
      bindkey -M $m '^[[3;5~' kill-word
      bindkey -M $m '^H' backward-kill-word      # Ctrl+Backspace arrives as 0x08
    fi
    if [[ $mode == alt || $mode == both ]]; then
      bindkey -M $m '^[[1;3D' backward-word
      bindkey -M $m '^[[1;3C' forward-word
      bindkey -M $m '^[[3;3~' kill-word
      bindkey -M $m '^[^?' backward-kill-word    # Alt+Backspace
    fi
  done
}
add-zsh-hook precmd _wate_bind_words

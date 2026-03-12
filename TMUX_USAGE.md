# tmux Usage with ts-cli

This guide shows practical ways to run `ts-cli` in `tmux` for split workflows.

## Prerequisites

- `tmux` installed
- `ts-cli` available in `PATH`

Quick checks:

```bash
command -v tmux
command -v ts-cli
```

## Start a tmux session

Create and attach to a named session:

```bash
tmux new -s tscli
```

Detach later with `Ctrl+b`, then `d`.

## Split panes and run ts-cli

Split horizontally (left/right) and run `ts-cli` in the new pane:

```bash
tmux split-window -h -c "#{pane_current_path}" "ts-cli"
```

Split vertically (top/bottom):

```bash
tmux split-window -v -c "#{pane_current_path}" "ts-cli"
```

## Useful pane controls

- Switch panes: `Ctrl+b`, then arrow keys
- Resize pane: `Ctrl+b`, then hold `Alt` + arrows
- Zoom active pane: `Ctrl+b`, then `z`
- Close pane: `exit` in the pane shell

## Suggested workflow

- Left pane: `ts-cli` interactive UI
- Right pane: logs, shell commands, or `tailscale status`

Example right pane command:

```bash
watch -n 2 tailscale status
```

## Optional shortcut

Add this to `~/.tmux.conf` to open `ts-cli` quickly in a right split:

```tmux
bind-key T split-window -h -c "#{pane_current_path}" "ts-cli"
```

Reload config:

```bash
tmux source-file ~/.tmux.conf
```

## Notes

- `Tab` and `Shift+Tab` are used inside `ts-cli` to switch internal frames.
- If your terminal does not send `Shift+Tab` reliably, use `Tab` for forward frame navigation.

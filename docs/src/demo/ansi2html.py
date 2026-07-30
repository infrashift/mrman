#!/usr/bin/env python3
"""Convert a `tmux capture-pane -p -e` dump into an HTML fragment of spans.

This is how `mrman-frame.html` — the terminal frame on the landing page — is
produced. It is a real capture of a running TUI, so regenerating it means
running mrman, not editing HTML.

    # Give it something with shape: a long file, a mid-file change.
    cd /tmp && rm -rf demo && mkdir -p demo/src && cd demo && git init -q
    # ... write a ~70-line src/cache.go, commit, then edit its middle ...

    # `env -u TMUX` is load-bearing: charm's colour detection downgrades to
    # 256 colours whenever TMUX is set, and the frame then renders in
    # approximated colours that look nothing like the theme.
    tmux new-session -d -s cap -x 104 -y 28 \\
        "env -u TMUX -u TMUX_PANE COLORTERM=truecolor TERM=xterm-256color \\
         mrman -w --theme tokyo-night-storm"
    sleep 4
    tmux send-keys -t cap "16j"; sleep 1        # onto a changed line
    tmux send-keys -t cap "c"; sleep 2          # comment
    tmux send-keys -t cap -l "Some review comment."; sleep 1
    tmux send-keys -t cap Enter; sleep 2
    tmux send-keys -t cap "5k"; sleep 1         # frame the comment nicely
    tmux capture-pane -p -e -t cap > /tmp/frame.ansi
    tmux kill-session -t cap

    python3 ansi2html.py < /tmp/frame.ansi > mrman-frame.html

The frame is a fixed 104-column capture; landing.css clamps the font size so
it never wraps. Changing the capture width means rechecking that clamp.
"""
import html
import re
import sys

def xterm256(n):
    if n < 16:
        base = [
            (0, 0, 0), (205, 0, 0), (0, 205, 0), (205, 205, 0),
            (0, 0, 238), (205, 0, 205), (0, 205, 205), (229, 229, 229),
            (127, 127, 127), (255, 0, 0), (0, 255, 0), (255, 255, 0),
            (92, 92, 255), (255, 0, 255), (0, 255, 255), (255, 255, 255),
        ]
        return base[n]
    if n < 232:
        n -= 16
        levels = [0, 95, 135, 175, 215, 255]
        return (levels[n // 36], levels[(n // 6) % 6], levels[n % 6])
    v = 8 + (n - 232) * 10
    return (v, v, v)

def hexcolor(n):
    r, g, b = xterm256(n)
    return f"#{r:02x}{g:02x}{b:02x}"

SGR = re.compile(r"\x1b\[([0-9;]*)m")

def convert(text):
    out = []
    for raw in text.split("\n"):
        fg = bg = None
        bold = italic = False
        open_span = False
        line = []

        def close():
            nonlocal open_span
            if open_span:
                line.append("</span>")
                open_span = False

        def opn():
            nonlocal open_span
            close()
            styles = []
            if fg:
                styles.append(f"color:{fg}")
            if bg:
                styles.append(f"background:{bg}")
            if bold:
                styles.append("font-weight:700")
            if italic:
                styles.append("font-style:italic")
            if styles:
                line.append(f'<span style="{";".join(styles)}">')
                open_span = True

        pos = 0
        for m in SGR.finditer(raw):
            chunk = raw[pos:m.start()]
            if chunk:
                line.append(html.escape(chunk))
            pos = m.end()
            params = [p for p in m.group(1).split(";") if p != ""] or ["0"]
            i = 0
            while i < len(params):
                p = int(params[i])
                if p == 0:
                    fg = bg = None
                    bold = italic = False
                elif p == 1:
                    bold = True
                elif p == 3:
                    italic = True
                elif p == 22:
                    bold = False
                elif p == 23:
                    italic = False
                elif p == 39:
                    fg = None
                elif p == 49:
                    bg = None
                elif p == 38 and i + 2 < len(params) and params[i + 1] == "5":
                    fg = hexcolor(int(params[i + 2]))
                    i += 2
                elif p == 48 and i + 2 < len(params) and params[i + 1] == "5":
                    bg = hexcolor(int(params[i + 2]))
                    i += 2
                elif p == 38 and i + 4 < len(params) and params[i + 1] == "2":
                    fg = "#%02x%02x%02x" % tuple(int(x) for x in params[i + 2:i + 5])
                    i += 4
                elif p == 48 and i + 4 < len(params) and params[i + 1] == "2":
                    bg = "#%02x%02x%02x" % tuple(int(x) for x in params[i + 2:i + 5])
                    i += 4
                elif 30 <= p <= 37:
                    fg = hexcolor(p - 30)
                elif 90 <= p <= 97:
                    fg = hexcolor(p - 90 + 8)
                elif 40 <= p <= 47:
                    bg = hexcolor(p - 40)
                i += 1
            opn()
        chunk = raw[pos:]
        if chunk:
            line.append(html.escape(chunk))
        close()
        out.append("".join(line).rstrip())
    return "\n".join(out)

if __name__ == "__main__":
    sys.stdout.write(convert(sys.stdin.read()))

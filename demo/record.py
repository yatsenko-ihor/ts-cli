#!/usr/bin/env python3
"""Records the ts-cli demo video from invented data.

It starts a fake Tailscale API (fake_api.py) and puts fake `tailscale`, `ssh`
and `scp` programs first on PATH (bin/), so no real machine, account, key or
network is involved. ts-cli runs in a pseudo-terminal, a script presses keys,
the screen is emulated with pyte, every step is rendered to an image with
Pillow, and ffmpeg turns the images into demo.mp4 and demo.gif.

    pip install pyte pillow           # ffmpeg must be installed too
    go build -o /tmp/ts-cli-demo .    # from the repository root
    python3 demo/record.py /tmp/ts-cli-demo docs/

Output: docs/demo.mp4, docs/demo.gif (and the frames in a temporary directory).
"""
import fcntl
import json
import os
import pty
import re
import select
import shutil
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

import pyte
from PIL import Image, ImageDraw, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
COLS, ROWS = 124, 46
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
FONT_B = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
CAPTION_FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_SIZE = 16
CAPTION_H = 54
DEFAULT_FG, DEFAULT_BG = "d7dae0", "14161b"

ANSI = {
    "black": "1e2127", "red": "e06c75", "green": "98c379", "brown": "e5c07b", "yellow": "e5c07b",
    "blue": "61afef", "magenta": "c678dd", "cyan": "56b6c2", "white": "d7dae0",
    "brightblack": "5c6370", "brightred": "ff7b86", "brightgreen": "b5e890", "brightbrown": "f0d58c",
    "brightyellow": "f0d58c", "brightblue": "7cc0ff", "brightmagenta": "d68cf0", "brightcyan": "70d0dc",
    "brightwhite": "ffffff",
}

EMOJI = {"🟢": ("#4caf50", False), "🔴": ("#ef5350", False), "⭕": ("#e8a33d", True)}

KEYS = {
    "up": b"\x1b[A", "down": b"\x1b[B", "right": b"\x1b[C", "left": b"\x1b[D", "enter": b"\r",
    "esc": b"\x1b", "tab": b"\t", "space": b" ", "f12": b"\x1b[24~", "ctrl_o": b"\x0f",
    "backspace": b"\x7f", "pgdn": b"\x1b[6~", "x": b"x", "f": b"f", "s": b"s", "q": b"q",
}


# ─── demo environment ────────────────────────────────────────────────────────

def make_home():
    home = "/home/demo"
    try:
        os.makedirs(home, exist_ok=True)
    except OSError:
        home = os.path.join(tempfile.gettempdir(), "ts-demo-home")
        os.makedirs(home, exist_ok=True)
    for path, size in {
        "Documents/report-2026-q1.pdf": 482_113, "Documents/invoice-0142.pdf": 98_240, "Documents/notes.md": 2_310,
        "Documents/contracts/": 0, "Downloads/installer.dmg": 52_000_000, "Projects/website/": 0,
        "Projects/api/": 0, "Pictures/": 0, "todo.txt": 640,
    }.items():
        full = os.path.join(home, path)
        if path.endswith("/"):
            os.makedirs(full, exist_ok=True)
        else:
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "wb") as f:
                f.write(b"\0" * min(size, 4096))
    return home


def make_config(root, keys):
    cfg = os.path.join(root, "config")
    shutil.rmtree(cfg, ignore_errors=True)
    os.makedirs(os.path.join(cfg, "scripts"))
    with open(os.path.join(cfg, "config.json"), "w") as f:
        json.dump({
            "accounts": [
                {"name": "work", "tailnet": "acme.example", "api_key": keys["acme.example"], "active": True},
                {"name": "home", "tailnet": "home.example", "api_key": keys["home.example"]},
            ],
            "ssh_username": "admin",
            "last_machine": "n0001DEMO",
            "machine_credentials": {
                f"n{i:04d}DEMO": {"name": n, "username": "admin"}
                for i, n in [(1, "web-01"), (2, "web-02"), (3, "web-03"), (4, "db-01"), (5, "build-mac")]
            },
            "commands": [{"name": "Uptime", "command": "uptime"}, {"name": "Free memory", "command": "free -h"}],
            "ui": {"theme": "dark", "tmux_split": "off"},
            "config_version": "1.0",
        }, f)
    with open(os.path.join(cfg, "scripts", "disk-usage.sh"), "w") as f:
        f.write("#!/usr/bin/env bash\n# ts-cli: name=Disk usage\n# ts-cli: desc=Used space per filesystem\n# ts-cli: os=linux,darwin\ndf -h\n")
    with open(os.path.join(cfg, "scripts", "restart-nginx.sh"), "w") as f:
        f.write("#!/usr/bin/env bash\n# ts-cli: name=Restart nginx\n# ts-cli: desc=Restart the web server\n# ts-cli: os=linux\nsudo systemctl restart nginx\n")
    return cfg


# ─── terminal emulation and rendering ────────────────────────────────────────

class Term:
    """Feeds ts-cli's output into pyte; the alternate screen is a second buffer."""
    SWITCH = re.compile(rb"\x1b\[\?(?:1049|1047|47)([hl])")

    def __init__(self, reply=lambda b: None):
        self.reply = reply  # answers the terminal queries ts-cli makes at start
        self.main = pyte.Screen(COLS, ROWS)
        self.alt = pyte.Screen(COLS, ROWS)
        self.stream = {id(s): pyte.ByteStream(s) for s in (self.main, self.alt)}
        self.cur = self.main

    def feed(self, data):
        for _ in re.finditer(rb"\x1b\]11;\?(?:\x07|\x1b\\)", data):
            r, g, b = (DEFAULT_BG[i:i + 2] * 2 for i in (0, 2, 4))
            self.reply(f"\x1b]11;rgb:{r}/{g}/{b}\x1b\\".encode())
        for _ in re.finditer(rb"\x1b\[6n", data):
            self.reply(b"\x1b[%d;%dR" % (self.cur.cursor.y + 1, self.cur.cursor.x + 1))
        for _ in re.finditer(rb"\x1b\[c", data):
            self.reply(b"\x1b[?62;c")
        pos = 0
        for m in self.SWITCH.finditer(data):
            self.stream[id(self.cur)].feed(data[pos:m.start()])
            if m.group(1) == b"h":
                self.cur = self.alt
                self.alt.reset()
            else:
                self.cur = self.main
            pos = m.end()
        self.stream[id(self.cur)].feed(data[pos:])


class Renderer:
    def __init__(self):
        self.font = ImageFont.truetype(FONT, FONT_SIZE)
        self.bold = ImageFont.truetype(FONT_B, FONT_SIZE)
        self.cap = ImageFont.truetype(CAPTION_FONT, 21)
        self.cw = round(self.font.getlength("M"))
        asc, desc = self.font.getmetrics()
        self.ch = asc + desc + 3
        self.w, self.h = COLS * self.cw, ROWS * self.ch + CAPTION_H
        self.w += self.w % 2

    @staticmethod
    def color(name, default):
        if name == "default":
            return default
        if name in ANSI:
            return "#" + ANSI[name]
        if re.fullmatch(r"[0-9a-fA-F]{6}", name):
            return "#" + name
        return default

    def render(self, screen, caption):
        img = Image.new("RGB", (self.w, self.h), "#" + DEFAULT_BG)
        d = ImageDraw.Draw(img)
        for y in range(ROWS):
            line = screen.buffer[y]
            for x in range(COLS):
                ch = line[x]
                fg = self.color(ch.fg, "#" + DEFAULT_FG)
                bg = self.color(ch.bg, "#" + DEFAULT_BG)
                if ch.reverse:
                    fg, bg = bg, fg
                px, py = x * self.cw, y * self.ch
                if bg != "#" + DEFAULT_BG:
                    d.rectangle([px, py, px + self.cw * (2 if len(ch.data) and ord(ch.data[0]) > 0x2E80 else 1) - 1, py + self.ch - 1], fill=bg)
                if ch.data in EMOJI:   # the font has no colour emoji: draw the circles
                    colour, ring = EMOJI[ch.data]
                    dia = self.cw - 1
                    box = [px + 1, py + 6, px + 1 + dia, py + 6 + dia]
                    d.ellipse(box, fill=None if ring else colour, outline=colour, width=2)
                elif ch.data.strip():
                    d.text((px, py), ch.data, font=self.bold if ch.bold else self.font, fill=fg)
        d.rectangle([0, ROWS * self.ch, self.w, self.h], fill="#0b0d11")
        if caption:
            tw = d.textlength(caption, font=self.cap)
            d.text(((self.w - tw) / 2, ROWS * self.ch + 12), caption, font=self.cap, fill="#f2f4f8")
        return img


# ─── the session ─────────────────────────────────────────────────────────────

class Session:
    def __init__(self, binary, env, frames_dir):
        self.rend, self.dir = Renderer(), frames_dir
        self.frames, self.caption, self.texts = [], "", []
        self.term = Term(lambda b: os.write(self.fd, b))
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execve(binary, [binary], env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def pump(self, quiet=0.2, maxwait=3.0):
        end, last = time.time() + maxwait, time.time()
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.05)
            if r:
                try:
                    data = os.read(self.fd, 65536)
                except OSError:
                    return False
                if not data:
                    return False
                self.term.feed(data)
                last = time.time()
            elif time.time() - last >= quiet:
                break
        return True

    def snap(self, hold=0.3):
        self.texts.append("\n".join(self.term.cur.display))  # the screen only: captions are written here
        img = self.rend.render(self.term.cur, self.caption)
        path = os.path.join(self.dir, f"{len(self.frames):05d}.png")
        img.save(path)
        self.frames.append((path, hold))

    def key(self, name, hold=0.35, settle=0.25, capture=True):
        os.write(self.fd, KEYS[name])
        self.pump(settle)
        if capture:
            self.snap(hold)

    def type(self, text, delay=0.07, hold=0.1, capture=True):
        for ch in text:
            os.write(self.fd, ch.encode())
            self.pump(0.04, 0.6)
            if capture:
                self.snap(hold)
            time.sleep(0)

    def hold(self, seconds):
        self.pump(0.1, 0.5)
        self.snap(seconds)

    def watch(self, seconds, every=0.35):
        end = time.time() + seconds
        while time.time() < end:
            self.pump(every, every + 0.2)
            self.snap(every)

    def say(self, text):
        self.caption = text

    def text(self):
        return "\n".join(self.term.cur.display)

    def close(self):
        try:
            os.kill(self.pid, signal.SIGTERM)
            os.waitpid(self.pid, 0)
        except OSError:
            pass


def scenario(s):
    s.pump(1.5, 6)
    s.key("x", capture=False)   # dismiss the "add to PATH" tip
    s.say("ts-cli: all your Tailscale machines in one terminal")
    s.hold(2.6)

    s.say("Details of the highlighted machine: path, traffic, key expiry, SSH setup")
    for _ in range(3):
        s.key("down", hold=0.55)
    s.hold(1.2)
    for _ in range(3):
        s.key("up", hold=0.25)

    s.say("Search as you type")
    s.key("backspace", capture=False) if False else None
    s.type("/")
    s.type("web", hold=0.25)
    s.hold(1.4)
    s.key("enter", hold=0.5)
    s.hold(1.0)
    s.key("esc", hold=0.6)
    s.hold(0.5)

    s.say("Mark machines, run a script on all of them in parallel")
    s.key("space", hold=0.5)
    s.key("down", hold=0.3)
    s.key("space", hold=0.8)
    s.key("tab", hold=0.9)
    s.key("enter", hold=0.3, settle=0.1)
    s.watch(3.2)
    s.hold(1.8)
    s.key("esc", hold=0.5)
    s.key("esc", hold=0.5)

    s.say("Ping with details: relay first, then a direct path")
    s.key("up", hold=0.3, capture=False)
    s.key("tab", hold=0.8)
    for _ in range(3):
        s.key("right", hold=0.5)
    s.key("enter", hold=0.2, settle=0.1)
    s.watch(2.6, 0.4)
    s.hold(1.6)
    s.say("Machine actions through the API: key expiry, rename, tags, delete")
    s.key("down", hold=0.6)
    s.hold(1.0)
    s.key("esc", hold=0.5)
    s.key("esc", hold=0.5)

    s.say("Themes: dark, dark-pastel, hacker, light, or your own (F12)")
    s.key("f12", hold=0.9)
    for _ in range(5):
        s.key("down", settle=0.08, capture=False)
    s.key("down", hold=0.9)
    s.key("enter", hold=1.1)                       # dark-pastel
    s.key("esc", hold=2.0)
    for want, hold in (("hacker", 2.2), ("dark", 1.0)):
        s.key("f12", settle=0.1, capture=False)
        for _ in range(6):
            s.key("down", settle=0.06, capture=False)
        s.key("enter", settle=0.1, capture=False)  # hacker; or light ...
        if want == "dark":
            s.key("enter", settle=0.1, capture=False)  # ... and then dark again
        s.key("esc", hold=hold)

    s.say("Upload files: Tab completes, Ctrl+O browses your disk")
    s.key("f", hold=0.3, capture=False) if False else s.type("f", hold=0.8)
    s.type("~/Doc", hold=0.15)
    s.key("tab", hold=1.3)
    s.key("ctrl_o", hold=1.0)
    s.key("down", hold=0.4)
    s.key("down", hold=0.4)
    s.key("enter", hold=0.9)          # the file is chosen
    s.key("enter", hold=0.9)          # confirm the local path -> asks for the remote one

    s.say("...and browse the machine itself over SSH")
    s.key("ctrl_o", hold=0.4, settle=0.9)
    s.hold(1.2)
    s.key("enter", hold=1.0)          # open backups/
    s.key("space", hold=1.0)          # choose this directory
    s.key("enter", hold=0.4, settle=0.1)
    s.watch(3.2)
    s.hold(1.6)
    s.key("esc", hold=0.4)
    s.key("esc", hold=1.8)

    s.say("Connect over SSH straight from the list")
    s.key("s", hold=0.4, settle=1.8)
    s.hold(1.2)
    s.type("uptime", hold=0.12)
    s.key("enter", hold=1.0)
    s.type("ls", hold=0.12)
    s.key("enter", hold=1.0)
    s.type("exit", hold=0.12)
    s.key("enter", hold=0.6, settle=1.2)
    s.hold(2.0)

    s.say("ts-cli: github.com/yatsenko-ihor/ts-cli")
    s.hold(3.0)
    return s


def build(binary, outdir):
    root = tempfile.mkdtemp(prefix="ts-demo-")
    frames = os.path.join(root, "frames")
    os.makedirs(frames)
    home = make_home()
    port_file = os.path.join(root, "port")
    api = subprocess.Popen([sys.executable, os.path.join(HERE, "fake_api.py"), port_file])
    for _ in range(100):
        if os.path.exists(port_file) and open(port_file).read():
            break
        time.sleep(0.05)
    sys.path.insert(0, HERE)
    import fake_api
    cfg = make_config(root, fake_api.KEYS)
    env = {
        "PATH": f"{HERE}/bin:/usr/local/bin:/usr/bin:/bin",
        "HOME": home, "USER": "demo", "LOGNAME": "demo",
        "TS_CLI_CONFIG_DIR": cfg,
        "TS_CLI_API_URL": f"http://127.0.0.1:{open(port_file).read()}",
        "TERM": "xterm-256color", "COLORTERM": "truecolor", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
    }
    s = Session(binary, env, frames)
    try:
        scenario(s)
        s.say("")
    finally:
        s.close()
        api.terminate()
    return s, root


# Nothing from the machine the video is recorded on may appear in it.
FORBIDDEN = re.compile(r"/home/user|/root|/tmp/|root@|yatsenko|ihor|claude|anthropic|sandbox|\.ts\.net/?\s*$", re.I)


def audit(s):
    """Fails when any recorded screen contains something that looks real."""
    bad = [(i, m.group(0)) for i, t in enumerate(s.texts) for m in [FORBIDDEN.search(t)] if m]
    if bad:
        raise SystemExit(f"audit failed: frames {bad[:5]} contain data that must not appear in the video")


def encode(s, outdir):
    os.makedirs(outdir, exist_ok=True)
    listing = os.path.join(os.path.dirname(s.frames[0][0]), "frames.txt")
    with open(listing, "w") as f:
        for path, hold in s.frames:
            f.write(f"file '{path}'\nduration {hold}\n")
        f.write(f"file '{s.frames[-1][0]}'\n")
    mp4 = os.path.join(outdir, "demo.mp4")
    gif = os.path.join(outdir, "demo.gif")
    subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", listing,
                    "-vf", "fps=25,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p", "-c:v", "libx264", "-crf", "26",
                    "-preset", "slow", "-movflags", "+faststart", mp4], check=True)
    subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", listing,
                    "-vf", "fps=8,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=96[p];[b][p]paletteuse=dither=bayer:bayer_scale=5",
                    gif], check=True)
    return mp4, gif


if __name__ == "__main__":
    binary, outdir = sys.argv[1], sys.argv[2]
    sess, root = build(binary, outdir)
    audit(sess)
    print(f"{len(sess.frames)} frames in {root}/frames; audit passed ({len(sess.texts)} screens checked)")
    if "--no-encode" not in sys.argv:
        print(*encode(sess, outdir))

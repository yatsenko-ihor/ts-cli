# Demo video

`docs/demo.mp4`/`.gif` (hacker theme; the `demo-dark.*` and `demo-light.*` variants) are recorded from **invented data only**. Nothing on the recording machine, no real account, key, machine name or address is involved:

- `fake_api.py` is a fake Tailscale API with made-up machines (`web-01`, `db-01`, `pi-kitchen`, ...), tailnets (`acme.example`, `home.example`) and keys. ts-cli is pointed at it with the `TS_CLI_API_URL` environment variable.
- `bin/tailscale`, `bin/ssh` and `bin/scp` are fake programs placed first on `PATH`: they print canned ping results, directory listings and a pretend shell, and run nothing real.
- `record.py` starts ts-cli in a pseudo-terminal, presses the keys of the script, emulates the screen (pyte), renders every step to an image (Pillow) and encodes the video (ffmpeg). Before encoding it **checks every recorded screen** for anything that looks like data from the recording machine (home directory, user name, temp paths) and refuses to produce the video if it finds some.

Re-record it (needs `ffmpeg`, `pip install pyte pillow`, and a writable `/home/demo`, else a temp directory is used for the demo home):

```bash
go build -o /tmp/ts-cli-demo .
python3 demo/record.py /tmp/ts-cli-demo docs/                  # docs/demo.mp4 + .gif (hacker theme, the default one)
python3 demo/record.py /tmp/ts-cli-demo docs/ --theme=dark     # docs/demo-dark.* (cycles through the themes)
python3 demo/record.py /tmp/ts-cli-demo docs/ --theme=light    # docs/demo-light.* (light terminal)
```

The hacker and light versions run the same script; instead of cycling the themes they show the settings window with the theme in use (the dark one cycles through them). The light theme does not paint a background, so that recording uses a light terminal background.

The script (`scenario()` in `record.py`) shows: the machine list with details, search, a script run on two machines in parallel, ping details and machine actions, themes, an upload with the local and the remote file browser, and an SSH session.

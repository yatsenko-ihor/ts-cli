// Package scripts discovers local scripts in ~/.ts-cli/scripts and builds the
// remote shell snippet that runs one of them on a machine over a single SSH
// connection, without copying it there first.
//
// A script is any regular file in the directory. Optional metadata lives in
// comment lines near the top of the file:
//
//	#!/usr/bin/env bash
//	# ts-cli: name=Disk usage
//	# ts-cli: desc=Largest directories under /
//	# ts-cli: os=linux,darwin
//	# ts-cli: args=path,depth
//
// Keys may also be combined on one line separated by ';'. "//" comments are
// accepted too, so JavaScript or Go-run scripts can carry the same header.
package scripts

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yatsenko-ihor/ts-cli/util"
)

// DirName is the scripts folder inside the config directory.
const DirName = "scripts"

// Script is a runnable local script with its parsed metadata.
type Script struct {
	Name        string   // Display name (metadata name= or the file name)
	Description string   // Optional one-line description
	File        string   // File name inside the scripts directory
	Path        string   // Absolute path
	OS          []string // Target OS filter (lower-case Tailscale OS names); empty = any
	Args        []string // Named positional arguments the user is asked for
	Content     []byte   // File contents, streamed to the remote host
}

// Dir returns the scripts directory path (not created).
func Dir() (string, error) {
	dir, err := util.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DirName), nil
}

// Load reads every regular file in the scripts directory. A missing directory
// is not an error: it yields an empty list.
func Load() ([]Script, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read scripts directory: %w", err)
	}

	var out []Script
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s := Parse(e.Name(), content)
		s.Path = path
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Parse extracts metadata from the first lines of content.
func Parse(fileName string, content []byte) Script {
	s := Script{
		Name:    fileName,
		File:    fileName,
		Content: content,
	}

	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < 30 && sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		for _, prefix := range []string{"#", "//"} {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			body := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if !strings.HasPrefix(body, "ts-cli:") {
				break
			}
			applyMeta(&s, strings.TrimSpace(strings.TrimPrefix(body, "ts-cli:")))
		}
	}
	return s
}

func applyMeta(s *Script, meta string) {
	for _, kv := range strings.Split(meta, ";") {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			if value != "" {
				s.Name = value
			}
		case "desc", "description":
			s.Description = value
		case "os":
			s.OS = splitList(value)
			for i := range s.OS {
				s.OS[i] = strings.ToLower(s.OS[i])
			}
		case "args":
			s.Args = splitList(value)
		}
	}
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SupportsOS reports whether the script is meant for the given Tailscale OS
// name (e.g. "linux", "macOS", "windows"). Scripts without an os= filter
// match everything.
func (s Script) SupportsOS(deviceOS string) bool {
	if len(s.OS) == 0 {
		return true
	}
	d := strings.ToLower(deviceOS)
	for _, o := range s.OS {
		if o == d || (o == "darwin" && d == "macos") || (o == "macos" && d == "darwin") {
			return true
		}
	}
	return false
}

// HasShebang reports whether the content starts with "#!".
func (s Script) HasShebang() bool {
	return bytes.HasPrefix(s.Content, []byte("#!"))
}

// RemoteCommand returns the POSIX shell snippet to pass as the single remote
// command to ssh. The script itself is expected on ssh's stdin. The snippet
// stores it in a private temp file, runs it with the given arguments, removes
// the file and propagates the exit status.
func (s Script) RemoteCommand(args []string) string {
	runner := `"$f"`
	if !s.HasShebang() {
		runner = `sh "$f"`
	}
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, ShellQuote(a))
	}
	argStr := ""
	if len(quoted) > 0 {
		argStr = " " + strings.Join(quoted, " ")
	}
	return fmt.Sprintf(
		`f=$(mktemp "${TMPDIR:-/tmp}/ts-cli.XXXXXX") || exit 97; cat >"$f" && chmod 700 "$f" && %s%s; rc=$?; rm -f "$f"; exit $rc`,
		runner, argStr)
}

// ShellQuote single-quotes s for a POSIX shell.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

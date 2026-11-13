package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.3.0", "v1.3.0", 0},
		{"1.2.9", "1.3.0", -1},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.3.0", "1.3.1", -1},
		{"1.3.0-rc1", "1.3.0", -1},
		{"1.3.0", "1.3.0-rc1", 1},
		{"1.3.0-rc1", "1.3.0-rc2", -1},
		{"1.3.0-rc2", "1.3.0-rc1", 1},
		{"1.3.0-rc1", "1.3.0-rc1", 0},
		{"dev", "1.0.0", -1},
		{"1.0.0", "dev", 1},
		{"dev", "unknown", 0},
		{"1.x.0", "1.0.0", -1},
		{"1.2", "1.0.0", -1},
		{"1.-2.0", "1.0.0", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestRelease(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotAccept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"tag_name":"v1.4.0","html_url":"https://x/r","assets":[
				{"name":"ts-cli_1.4.0_linux_amd64.tar.gz","browser_download_url":"https://x/a","size":5},
				{"name":"ts-cli_1.4.0_windows_amd64.zip","browser_download_url":"https://x/w","size":6},
				{"name":"checksums.txt","browser_download_url":"https://x/c","size":7}]}`))
		case "/notag":
			w.Write([]byte(`{}`))
		case "/junk":
			w.Write([]byte(`not json`))
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	old := releasesURL
	t.Cleanup(func() { releasesURL = old })

	releasesURL = srv.URL + "/ok"
	rel, err := LatestRelease()
	if err != nil || rel.Tag != "v1.4.0" || len(rel.Assets) != 3 || gotUA != "ts-cli" || !strings.Contains(gotAccept, "json") {
		t.Fatalf("%+v %v ua=%q accept=%q", rel, err, gotUA, gotAccept)
	}
	if a, ok := rel.AssetFor("linux", "amd64"); !ok || a.URL != "https://x/a" {
		t.Fatalf("linux asset: %+v %v", a, ok)
	}
	if a, ok := rel.AssetFor("windows", "amd64"); !ok || a.URL != "https://x/w" {
		t.Fatalf("windows asset: %+v %v", a, ok)
	}
	if _, ok := rel.AssetFor("linux", "riscv64"); ok {
		t.Fatal("no asset for riscv64")
	}
	if a, ok := rel.Asset("checksums.txt"); !ok || a.Size != 7 {
		t.Fatal("checksums asset")
	}
	if _, ok := rel.Asset("nope"); ok {
		t.Fatal("missing asset")
	}

	for path, want := range map[string]string{"/notag": "no tag", "/junk": "unexpected answer", "/missing": "404"} {
		releasesURL = srv.URL + path
		if _, err := LatestRelease(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
	releasesURL = "http://127.0.0.1:1/unreachable"
	if _, err := LatestRelease(); err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Fatalf("unreachable: %v", err)
	}
	releasesURL = "http://bad host/"
	if _, err := LatestRelease(); err == nil {
		t.Fatal("bad URL")
	}
}

func TestReleasesURLOverride(t *testing.T) {
	if apiRoot2(" https://mirror.example/latest ", "d") != "https://mirror.example/latest" || apiRoot2("ftp://x", "d") != "d" || apiRoot2("", "d") != "d" {
		t.Fatal("only http(s) overrides are accepted")
	}
}

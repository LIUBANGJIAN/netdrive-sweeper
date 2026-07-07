package main

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]string{
		"http://192.168.1.2:19798/": "192.168.1.2:19798",
		"192.168.1.2":               "192.168.1.2:19798",
		" 192.168.1.2:19798 ":       "192.168.1.2:19798",
	}
	for in, want := range cases {
		if got := normalizeAddress(in); got != want {
			t.Fatalf("normalizeAddress(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizePathAndDisplayPath(t *testing.T) {
	if got := normalizePath("电影/测试"); got != "/电影/测试" {
		t.Fatalf("normalizePath got %q", got)
	}
	token := &TokenInfo{RootDir: "/BON_115网盘"}
	if got := displayPath(token, "/电影"); got != "/BON_115网盘/电影" {
		t.Fatalf("displayPath got %q", got)
	}
}

func TestShouldClean(t *testing.T) {
	cfg := defaultConfig()
	if !shouldClean(FileItem{Name: "ad.url", Size: 1}, cfg) {
		t.Fatal("ad file should match")
	}
	if !shouldClean(FileItem{Name: "sample.mp4", Size: 1024}, cfg) {
		t.Fatal("small video should match")
	}
	if shouldClean(FileItem{Name: "movie.mp4", Size: 100 * 1024 * 1024}, cfg) {
		t.Fatal("large video should not match")
	}
}

func TestOfflineStatusName(t *testing.T) {
	cases := map[protoreflect.EnumNumber]string{0: "unknown", 1: "finished", 2: "error", 3: "downloading", 99: "unknown"}
	for in, want := range cases {
		if got := offlineStatusName(in); got != want {
			t.Fatalf("offlineStatusName(%d)=%q want %q", in, got, want)
		}
	}
}

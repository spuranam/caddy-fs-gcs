package errorpages

import (
	"io/fs"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestCaddyModule(t *testing.T) {
	var ep EmbeddedErrorPages
	info := ep.CaddyModule()
	if info.ID != "caddy.fs.error_pages" {
		t.Fatalf("ID = %q, want %q", info.ID, "caddy.fs.error_pages")
	}
	m := info.New()
	if _, ok := m.(*EmbeddedErrorPages); !ok {
		t.Fatalf("New() returned %T, want *EmbeddedErrorPages", m)
	}
}

func TestProvision(t *testing.T) {
	var ep EmbeddedErrorPages
	if err := ep.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	if ep.FS == nil {
		t.Fatal("FS is nil after Provision")
	}

	// Verify embedded files are accessible.
	for _, name := range []string{"404.html", "403.html", "500.html", "default.html"} {
		f, err := ep.Open(name)
		if err != nil {
			t.Errorf("Open(%q) error: %v", name, err)
			continue
		}
		fi, err := f.Stat()
		f.Close()
		if err != nil {
			t.Errorf("Stat(%q) error: %v", name, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// Non-existent file should fail.
	_, err := ep.Open("nope.html")
	if err == nil {
		t.Error("Open(nope.html) should fail")
	}
}

func TestUnmarshalCaddyfileEmpty(t *testing.T) {
	d := caddyfile.NewTestDispenser(`error_pages`)
	var ep EmbeddedErrorPages
	if err := ep.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile() error: %v", err)
	}
}

func TestUnmarshalCaddyfileRejectsBlock(t *testing.T) {
	d := caddyfile.NewTestDispenser(`error_pages {
		something
	}`)
	var ep EmbeddedErrorPages
	if err := ep.UnmarshalCaddyfile(d); err == nil {
		t.Fatal("expected error for block content")
	}
}

func TestInterfaceAssertions(t *testing.T) {
	var ep EmbeddedErrorPages
	_ = fs.FS(&ep)
	_ = caddy.Module(&ep)
	_ = caddy.Provisioner(&ep)
	_ = caddyfile.Unmarshaler(&ep)
}

package launchagent

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestRenderPlistContainsExpectedKeys(t *testing.T) {
	data, err := RenderPlist(Config{
		Label:       "com.diegoavila.exo",
		ProgramPath: "/usr/local/bin/exo",
		SocketName:  "listener",
		Port:        45873,
	})
	if err != nil {
		t.Fatalf("render plist failed: %v", err)
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		if _, err := decoder.Token(); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("plist is not well-formed xml: %v", err)
		}
	}

	text := string(data)
	for _, want := range []string{
		"<key>Label</key>",
		"<key>ProgramArguments</key>",
		"<key>Sockets</key>",
		"<key>SockNodeName</key>",
		"<key>SockServiceName</key>",
		"<key>SockFamily</key>",
		"<string>127.0.0.1</string>",
		"<string>45873</string>",
		"<string>serve</string>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist missing %q in %s", want, text)
		}
	}
}

func TestRenderPlistIncludesEnvironmentVariablesWhenProvided(t *testing.T) {
	data, err := RenderPlist(Config{
		Label:       "com.diegoavila.exo",
		ProgramPath: "/usr/local/bin/exo",
		SocketName:  "listener",
		Port:        45873,
		EnvironmentVariables: map[string]string{
			"PATH": "/opt/homebrew/bin:/tmp/a&b/bin",
		},
	})
	if err != nil {
		t.Fatalf("render plist failed: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"<key>EnvironmentVariables</key>",
		"<key>PATH</key>",
		"<string>/opt/homebrew/bin:/tmp/a&amp;b/bin</string>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist missing %q in %s", want, text)
		}
	}
}

func TestRenderPlistOmitsEnvironmentVariablesWhenEmpty(t *testing.T) {
	data, err := RenderPlist(Config{
		Label:       "com.diegoavila.exo",
		ProgramPath: "/usr/local/bin/exo",
		SocketName:  "listener",
		Port:        45873,
	})
	if err != nil {
		t.Fatalf("render plist failed: %v", err)
	}

	if strings.Contains(string(data), "<key>EnvironmentVariables</key>") {
		t.Fatalf("plist unexpectedly included EnvironmentVariables: %s", string(data))
	}
}

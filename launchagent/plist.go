package launchagent

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	Label                string
	ProgramPath          string
	SocketName           string
	Port                 int
	EnvironmentVariables map[string]string
}

func RenderPlist(config Config) ([]byte, error) {
	if config.Label == "" || config.ProgramPath == "" || config.SocketName == "" || config.Port <= 0 {
		return nil, fmt.Errorf("launchagent: invalid config")
	}
	environmentVariables := renderEnvironmentVariables(config.EnvironmentVariables)
	payload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>Sockets</key>
  <dict>
    <key>%s</key>
    <dict>
      <key>SockNodeName</key>
      <string>127.0.0.1</string>
      <key>SockServiceName</key>
      <string>%s</string>
      <key>SockFamily</key>
      <string>IPv4</string>
      <key>SockType</key>
      <string>stream</string>
      <key>SockProtocol</key>
      <string>TCP</string>
    </dict>
  </dict>
%s
</dict>
</plist>
`, xmlEscape(config.Label), xmlEscape(config.ProgramPath), xmlEscape(config.SocketName), strconv.Itoa(config.Port), environmentVariables)
	decoder := xml.NewDecoder(bytes.NewBufferString(payload))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return []byte(payload), nil
}

func DomainTarget(uid int) string {
	return fmt.Sprintf("gui/%d", uid)
}

func BootstrapArgs(uid int, plistPath string) []string {
	return []string{"bootstrap", DomainTarget(uid), plistPath}
}

func BootoutArgs(uid int, plistPath string) []string {
	return []string{"bootout", DomainTarget(uid), plistPath}
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func renderEnvironmentVariables(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString("  <key>EnvironmentVariables</key>\n")
	builder.WriteString("  <dict>\n")
	for _, key := range keys {
		builder.WriteString("    <key>")
		builder.WriteString(xmlEscape(key))
		builder.WriteString("</key>\n")
		builder.WriteString("    <string>")
		builder.WriteString(xmlEscape(values[key]))
		builder.WriteString("</string>\n")
	}
	builder.WriteString("  </dict>")
	return builder.String()
}

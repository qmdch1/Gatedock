// Package sshconfig imports a conservative, explicit subset of OpenSSH configuration.
package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type Candidate struct {
	Alias        string   `json:"alias"`
	Address      string   `json:"address"`
	Port         int      `json:"port"`
	Username     string   `json:"username"`
	IdentityFile string   `json:"identity_file"`
	Jumps        []string `json:"jumps"`
	Warnings     []string `json:"warnings"`
	Valid        bool     `json:"valid"`
}
type Preview struct {
	Hosts    []Candidate `json:"hosts"`
	Warnings []string    `json:"warnings"`
}
type directive struct {
	patterns []string
	key      string
	values   []string
	line     int
}

func tokens(line string) ([]string, error) {
	out := []string{}
	var b strings.Builder
	var quote rune
	for _, c := range line {
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteRune(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' {
			break
		}
		if c == ' ' || c == '\t' || c == '=' {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(c)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote")
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out, nil
}
func matches(patterns []string, alias string) bool {
	matched := false
	for _, p := range patterns {
		negative := strings.HasPrefix(p, "!")
		p = strings.TrimPrefix(p, "!")
		ok, _ := path.Match(strings.ToLower(p), strings.ToLower(alias))
		if ok && negative {
			return false
		}
		matched = matched || ok
	}
	return matched
}
func Parse(r io.Reader) (Preview, error) {
	out := Preview{Hosts: []Candidate{}, Warnings: []string{}}
	scan := bufio.NewScanner(io.LimitReader(r, 2*1024*1024))
	scan.Buffer(make([]byte, 4096), 128*1024)
	patterns := []string{"*"}
	directives := []directive{}
	aliases := []string{}
	seen := map[string]bool{}
	inMatch := false
	for n := 1; scan.Scan(); n++ {
		parts, e := tokens(scan.Text())
		if e != nil {
			return out, fmt.Errorf("line %d: %w", n, e)
		}
		if len(parts) == 0 {
			continue
		}
		key := strings.ToLower(parts[0])
		values := parts[1:]
		if len(values) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("line %d: 값이 없는 지시문 무시", n))
			continue
		}
		if key == "host" {
			patterns = values
			inMatch = false
			for _, a := range values {
				if !strings.ContainsAny(a, "*?![]") && !seen[a] {
					seen[a] = true
					aliases = append(aliases, a)
				}
			}
		}
		if key == "match" {
			inMatch = true
			out.Warnings = append(out.Warnings, "Match 조건은 가져오지 않습니다. 관련 호스트는 수동 검토가 필요합니다")
		}
		if key == "include" {
			out.Warnings = append(out.Warnings, "Include 파일은 자동으로 읽지 않습니다. 포함 파일을 별도로 가져오세요")
		}
		if inMatch || key == "host" || key == "match" {
			continue
		}
		directives = append(directives, directive{patterns, key, values, n})
	}
	if e := scan.Err(); e != nil {
		return out, e
	}
	for _, alias := range aliases {
		values := map[string]string{}
		warnings := []string{}
		for _, d := range directives {
			if !matches(d.patterns, alias) {
				continue
			}
			if _, ok := values[d.key]; !ok {
				values[d.key] = strings.Join(d.values, " ")
			}
		}
		c := Candidate{Alias: alias, Address: values["hostname"], Port: 22, Username: values["user"], IdentityFile: values["identityfile"], Jumps: []string{}, Warnings: warnings, Valid: true}
		if c.Address == "" {
			c.Address = alias
		}
		if values["port"] != "" {
			p, e := strconv.Atoi(values["port"])
			if e != nil || p < 1 || p > 65535 {
				c.Warnings = append(c.Warnings, "유효하지 않은 Port")
				c.Valid = false
			} else {
				c.Port = p
			}
		}
		if c.Username == "" || c.IdentityFile == "" {
			c.Warnings = append(c.Warnings, "User와 IdentityFile을 명시해야 합니다")
			c.Valid = false
		}
		if strings.Contains(c.IdentityFile, "%") || strings.Contains(c.Address, "%") || strings.Contains(c.IdentityFile, "${") {
			c.Warnings = append(c.Warnings, "% 토큰/환경 변수 확장은 지원하지 않습니다")
			c.Valid = false
		}
		if j := values["proxyjump"]; j != "" && j != "none" {
			for _, hop := range strings.Split(j, ",") {
				hop = strings.TrimSpace(hop)
				if hop == "" || strings.ContainsAny(hop, "@: /\\") {
					c.Warnings = append(c.Warnings, "ProxyJump는 등록된 Host 별칭만 지원합니다")
					c.Valid = false
				}
				c.Jumps = append(c.Jumps, hop)
			}
		}
		for _, k := range []string{"proxycommand", "localforward", "remoteforward", "dynamicforward", "certificatefile", "identityagent", "include"} {
			if v := values[k]; v != "" && v != "none" {
				c.Warnings = append(c.Warnings, k+" 지시문은 가져오지 않습니다")
				if k == "proxycommand" || k == "certificatefile" || k == "identityagent" || k == "include" {
					c.Valid = false
				}
			}
		}
		// Conditional routing can change ProxyJump or identity. Never silently bypass it.
		for _, warning := range out.Warnings {
			if strings.HasPrefix(warning, "Match ") {
				c.Valid = false
				c.Warnings = append(c.Warnings, "Match 조건이 있는 파일은 수동으로 단순화한 복사본을 가져오세요")
				break
			}
		}
		out.Hosts = append(out.Hosts, c)
	}
	return out, nil
}

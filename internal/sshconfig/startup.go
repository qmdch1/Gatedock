package sshconfig

import (
	"errors"
	"os"
	"strings"

	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
)

type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type StartupResult struct {
	Source   string    `json:"source"`
	Imported int       `json:"imported"`
	Existing int       `json:"existing"`
	Skipped  []Skipped `json:"skipped"`
	Warnings []string  `json:"warnings"`
	Missing  bool      `json:"missing"`
	Disabled bool      `json:"disabled"`
}

// AutoImport adds new aliases at startup. It never dials SSH or changes existing
// records. A connected group of new jump hosts is committed as one transaction.
func AutoImport(store *database.Store, path string) StartupResult {
	result := StartupResult{Source: path, Skipped: []Skipped{}, Warnings: []string{}}
	p, e := Read(path)
	if errors.Is(e, os.ErrNotExist) {
		result.Missing = true
		return result
	}
	if e != nil {
		result.Warnings = append(result.Warnings, "SSH config를 읽거나 해석하지 못했습니다. 파일 경로와 형식을 확인하세요.")
		return result
	}
	result.Warnings = append(result.Warnings, p.Warnings...)
	state, e := store.Snapshot()
	if e != nil {
		result.Warnings = append(result.Warnings, "설정 저장소를 읽지 못했습니다.")
		return result
	}
	existing := map[string]bool{}
	for _, h := range state.Hosts {
		existing[h.Name] = true
	}
	eligible := map[string]Candidate{}
	for _, c := range p.Hosts {
		if existing[c.Alias] {
			result.Existing++
			continue
		}
		reason := ""
		if !c.Valid {
			reason = strings.Join(c.Warnings, "; ")
		} else {
			h := model.Host{Name: c.Alias, Environment: "DEV", Type: "Server", Address: c.Address, Port: c.Port, Username: c.Username, KeyID: "pending"}
			if e := model.ValidateHost(h); e != nil {
				reason = e.Error()
			} else if path, e := platform.Path(c.IdentityFile); e != nil {
				reason = "키 파일 경로를 확인하세요."
			} else if e = key.Validate(path); e != nil {
				reason = e.Error()
			}
		}
		if reason != "" {
			result.Skipped = append(result.Skipped, Skipped{c.Alias, reason})
			continue
		}
		eligible[c.Alias] = c
	}
	// Remove descendants of unsupported/missing jumps, without blocking unrelated hosts.
	for changed := true; changed; {
		changed = false
		for _, c := range p.Hosts {
			if _, ok := eligible[c.Alias]; !ok {
				continue
			}
			for _, hop := range c.Jumps {
				if _, ok := eligible[hop]; !ok && !existing[hop] {
					delete(eligible, c.Alias)
					result.Skipped = append(result.Skipped, Skipped{c.Alias, "Jump Host가 없거나 가져올 수 없습니다: " + hop})
					changed = true
					break
				}
			}
		}
	}
	neighbors := map[string][]string{}
	for _, c := range p.Hosts {
		if _, ok := eligible[c.Alias]; !ok {
			continue
		}
		for _, hop := range c.Jumps {
			if _, ok := eligible[hop]; ok {
				neighbors[c.Alias] = append(neighbors[c.Alias], hop)
				neighbors[hop] = append(neighbors[hop], c.Alias)
			}
		}
	}
	visited := map[string]bool{}
	for _, c := range p.Hosts {
		if _, ok := eligible[c.Alias]; !ok || visited[c.Alias] {
			continue
		}
		group := []string{}
		queue := []string{c.Alias}
		selected := map[string]bool{}
		for len(queue) > 0 {
			alias := queue[0]
			queue = queue[1:]
			if visited[alias] {
				continue
			}
			visited[alias] = true
			selected[alias] = true
			group = append(group, alias)
			queue = append(queue, neighbors[alias]...)
		}
		e = store.Update(func(state *model.State) error { return ApplySelected(state, p, selected) })
		if e != nil {
			for _, alias := range group {
				result.Skipped = append(result.Skipped, Skipped{alias, e.Error()})
			}
			continue
		}
		result.Imported += len(group)
	}
	return result
}

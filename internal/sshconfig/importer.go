package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"sshdesk/internal/database"
	"sshdesk/internal/key"
	"sshdesk/internal/model"
	"sshdesk/internal/platform"
)

// Read only reads the selected SSH config; it never rewrites the source.
func Read(path string) (Preview, error) {
	p, e := platform.Path(path)
	if e != nil {
		return Preview{}, e
	}
	f, e := os.Open(p)
	if e != nil {
		return Preview{}, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return Preview{}, errors.New("config는 2 MiB 이하의 일반 파일이어야 합니다")
	}
	return Parse(f)
}

// ApplySelected must run inside Store.Update, which validates and commits atomically.
func ApplySelected(state *model.State, p Preview, selected map[string]bool) error {

	byName := map[string]string{}
	for _, h := range state.Hosts {
		byName[h.Name] = h.ID
	}
	newHosts := map[string]int{}
	for _, c := range p.Hosts {
		if !selected[c.Alias] {
			continue
		}
		if !c.Valid {
			return fmt.Errorf("%s: 지원하지 않는 config입니다", c.Alias)
		}
		if byName[c.Alias] != "" {
			return fmt.Errorf("%s: 이미 등록된 Host입니다", c.Alias)
		}
		path, e := platform.Path(c.IdentityFile)
		if e != nil {
			return e
		}
		if e = key.Validate(path); e != nil {
			return fmt.Errorf("%s: %w", c.Alias, e)
		}
		keyID := ""
		for _, k := range state.Keys {
			if k.Path == path {
				keyID = k.ID
			}
		}
		if keyID == "" {
			keyID = database.ID()
			state.Keys = append(state.Keys, model.Key{ID: keyID, Name: c.Alias + " key", Path: path})
		}
		h := model.Host{ID: database.ID(), Name: c.Alias, Environment: "DEV", Type: "Server", Address: c.Address, Port: c.Port, Username: c.Username, KeyID: keyID}
		byName[c.Alias] = h.ID
		newHosts[c.Alias] = len(state.Hosts)
		state.Hosts = append(state.Hosts, h)
	}
	if len(newHosts) != len(selected) {
		return errors.New("선택한 별칭을 config에서 찾을 수 없습니다")
	}
	for _, c := range p.Hosts {
		index, ok := newHosts[c.Alias]
		if !ok {
			continue
		}
		previous := ""
		for _, hop := range c.Jumps {
			id := byName[hop]
			if id == "" {
				return fmt.Errorf("%s: Jump Host %s도 선택하거나 먼저 등록하세요", c.Alias, hop)
			}
			if previous != "" {
				if i, exists := newHosts[hop]; exists {
					if state.Hosts[i].JumpID != "" && state.Hosts[i].JumpID != previous {
						return errors.New("ProxyJump 경로 충돌")
					}
					state.Hosts[i].JumpID = previous
				} else {
					h, _ := state.Host(id)
					if h.JumpID != previous {
						return errors.New("기존 Jump Host 경로와 config가 충돌합니다")
					}
				}
			}
			previous = id
		}
		if previous != "" {
			if state.Hosts[index].JumpID != "" && state.Hosts[index].JumpID != previous {
				return errors.New("ProxyJump 경로 충돌")
			}
			state.Hosts[index].JumpID = previous
		}
	}
	return nil
}

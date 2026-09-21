package web

import (
	"errors"
	"sshdesk/internal/database"
	"sshdesk/internal/model"
)

// Keep the existing service records for backward-compatible databases and backups.
// The UI manages these records exclusively as tunnel web links.
func replaceTunnelLinks(state *model.State, tunnel model.Tunnel, links []model.Service) error {
	existing := map[string]model.Service{}
	result := []model.Service{}
	for _, link := range state.Services {
		if link.TunnelID == tunnel.ID {
			existing[link.ID] = link
		} else {
			result = append(result, link)
		}
	}
	host, err := state.Host(tunnel.HostID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, link := range links {
		if link.ID != "" {
			old, ok := existing[link.ID]
			if !ok || seen[link.ID] {
				return errors.New("잘못된 웹 바로가기 ID입니다")
			}
			link.Environment = old.Environment
			link.Description = old.Description
		} else {
			link.ID = database.ID()
			link.Environment = host.Environment
		}
		seen[link.ID] = true
		link.TunnelID = tunnel.ID
		if err := model.ValidateService(link, tunnel); err != nil {
			return err
		}
		result = append(result, link)
	}
	state.Services = result
	return nil
}

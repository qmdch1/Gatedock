package database

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"sshdesk/internal/model"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Open(path string, defaults model.Settings) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); e != nil {
		db.Close()
		return nil, e
	}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	if version > 2 {
		db.Close()
		return nil, errors.New("database version is newer than this application")
	}
	if version < 1 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, e
		}
		raw, _ := migrations.ReadFile("migrations/001_initial.sql")
		if _, e = tx.Exec(string(raw)); e == nil {
			_, e = tx.Exec("PRAGMA user_version=1")
		}
		if e != nil {
			tx.Rollback()
			db.Close()
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			db.Close()
			return nil, e
		}
	}
	if version < 2 {
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}
		raw, _ := migrations.ReadFile("migrations/002_team.sql")
		if _, err = tx.Exec(string(raw)); err == nil {
			_, err = tx.Exec("PRAGMA user_version=2")
		}
		if err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			db.Close()
			return nil, err
		}
	}
	_ = os.Chmod(path, 0600)
	var count int
	if e = db.QueryRow("SELECT count(*) FROM settings").Scan(&count); e != nil {
		db.Close()
		return nil, e
	}
	if count == 0 {
		b, _ := json.Marshal(defaults)
		if _, e = db.Exec("INSERT INTO settings(id,data) VALUES('global',?)", b); e != nil {
			db.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func read[T any](db *sql.DB, table string) ([]T, error) {
	rows, e := db.Query("SELECT data FROM " + table + " ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b []byte
		var v T
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) snapshot() (out model.State, e error) {
	if out.Team, e = read[model.TeamSubscription](s.db, "team_sources"); e != nil {
		return
	}
	if out.Hosts, e = read[model.Host](s.db, "hosts"); e != nil {
		return
	}
	if out.Keys, e = read[model.Key](s.db, "ssh_keys"); e != nil {
		return
	}
	if out.Tunnels, e = read[model.Tunnel](s.db, "tunnels"); e != nil {
		return
	}
	if out.Services, e = read[model.Service](s.db, "services"); e != nil {
		return
	}
	if out.History, e = read[model.History](s.db, "connection_history"); e != nil {
		return
	}
	var sets []model.Settings
	sets, e = read[model.Settings](s.db, "settings")
	if len(sets) > 0 {
		out.Settings = sets[0]
	}
	sort.Slice(out.History, func(i, j int) bool { return out.History[i].At > out.History[j].At })
	return
}
func (s *Store) Snapshot() (model.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}
func Validate(s model.State) error {
	names := map[string]bool{}
	for _, k := range s.Keys {
		if !model.Name(k.Name) || k.Path == "" {
			return errors.New("키 이름과 파일 경로가 필요합니다")
		}
	}
	for _, h := range s.Hosts {
		if e := model.ValidateHost(h); e != nil {
			return e
		}
		if names[h.Name] {
			return errors.New("Host 이름이 중복됩니다")
		}
		names[h.Name] = true
		if _, e := s.Key(h.KeyID); e != nil {
			return e
		}
		if _, e := s.Chain(h.ID); e != nil {
			return e
		}
	}
	for _, t := range s.Tunnels {
		if e := model.ValidateTunnel(t); e != nil {
			return e
		}
		if _, e := s.Host(t.HostID); e != nil {
			return e
		}
	}
	for _, v := range s.Services {
		t, e := s.Tunnel(v.TunnelID)
		if e != nil {
			return e
		}
		if e = model.ValidateService(v, t); e != nil {
			return e
		}
	}
	return nil
}
func write[T any](tx *sql.Tx, table string, values []T, id func(T) string) error {
	if _, e := tx.Exec("DELETE FROM " + table); e != nil {
		return e
	}
	for _, v := range values {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO "+table+"(id,data) VALUES(?,?)", id(v), b); e != nil {
			return e
		}
	}
	return nil
}

// Update serializes read/validate/write, including reference checks, in a transaction.
func (s *Store) Update(fn func(*model.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, e := s.snapshot()
	if e != nil {
		return e
	}
	if e = fn(&state); e != nil {
		return e
	}
	if e = Validate(state); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	actions := []func() error{func() error { return write(tx, "hosts", state.Hosts, func(v model.Host) string { return v.ID }) }, func() error { return write(tx, "ssh_keys", state.Keys, func(v model.Key) string { return v.ID }) }, func() error { return write(tx, "tunnels", state.Tunnels, func(v model.Tunnel) string { return v.ID }) }, func() error {
		return write(tx, "services", state.Services, func(v model.Service) string { return v.ID })
	}, func() error {
		return write(tx, "settings", []model.Settings{state.Settings}, func(v model.Settings) string { return "global" })
	}}
	actions = append(actions, func() error {
		return write(tx, "team_sources", state.Team, func(v model.TeamSubscription) string { return v.Source.Feed })
	})
	for _, action := range actions {
		if e = action(); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// LocalValue stores application identity and feed scopes outside exported state.
func (s *Store) LocalValue(id string, create func() ([]byte, error)) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var data []byte
	err := s.db.QueryRow("SELECT data FROM local_metadata WHERE id=?", id).Scan(&data)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, sql.ErrNoRows) || create == nil {
		return nil, err
	}
	data, err = create()
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec("INSERT INTO local_metadata(id,data) VALUES(?,?)", id, data)
	return data, err
}
func (s *Store) Record(h model.Host, kind string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := model.History{ID: ID(), HostID: h.ID, HostName: h.Name, Kind: kind, Status: "connected", At: time.Now().UTC().Format(time.RFC3339Nano)}
	if err != nil {
		v.Status = "error"
		v.Error = err.Error()
	}
	b, e := json.Marshal(v)
	if e == nil {
		_, _ = s.db.Exec("INSERT INTO connection_history(id,data) VALUES(?,?)", v.ID, b)
		_, _ = s.db.Exec("DELETE FROM connection_history WHERE rowid NOT IN (SELECT rowid FROM connection_history ORDER BY rowid DESC LIMIT 200)")
	}
}
func Replace[T any](xs []T, id string, value T, getID func(T) string) ([]T, error) {
	if id == "" {
		return append(xs, value), nil
	}
	for i, v := range xs {
		if getID(v) == id {
			xs[i] = value
			return xs, nil
		}
	}
	return xs, fmt.Errorf("record not found: %s", id)
}
func Delete[T any](xs []T, id string, getID func(T) string) ([]T, error) {
	for i, v := range xs {
		if getID(v) == id {
			return append(xs[:i], xs[i+1:]...), nil
		}
	}
	return xs, errors.New("record not found")
}

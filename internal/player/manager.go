package player

import (
	"log/slog"
	"sync"

	"github.com/disgoorg/snowflake/v2"

	"github.com/blueshapes/gomusic/internal/audio"
)

// Manager keeps one player per guild.
type Manager struct {
	voice Voice
	cache *audio.Cache
	log   *slog.Logger

	mu      sync.Mutex
	players map[snowflake.ID]*Player
}

func NewManager(voice Voice, cache *audio.Cache, log *slog.Logger) *Manager {
	return &Manager{voice: voice, cache: cache, log: log, players: make(map[snowflake.ID]*Player)}
}

func (m *Manager) GetOrCreate(guildID snowflake.ID) *Player {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.players[guildID]; ok && !p.Destroyed() {
		return p
	}
	p := New(guildID, m.voice, m.cache, m.log)
	m.players[guildID] = p
	return p
}

func (m *Manager) Get(guildID snowflake.ID) (*Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[guildID]
	return p, ok
}

func (m *Manager) Remove(guildID snowflake.ID) {
	m.mu.Lock()
	p, ok := m.players[guildID]
	delete(m.players, guildID)
	m.mu.Unlock()
	if ok {
		p.Stop()
	}
}

package recommender

import (
	"context"
	"fmt"
	"log"
	"sync"
)

type StateManager struct {
	Mu              sync.RWMutex
	Users           map[string]string
	UserFavorites   map[string]map[string]bool
	Config          *Config
	UserCollections map[string]*UserCollectionInfo
}

func NewStateManager(cfg *Config) *StateManager {
	return &StateManager{
		Config:          cfg,
		UserFavorites:   make(map[string]map[string]bool),
		Users:           make(map[string]string),
		UserCollections: make(map[string]*UserCollectionInfo),
	}
}

func (s *StateManager) Sync(ctx context.Context) error {
	users, err := s.getUsers()
	if err != nil {
		return fmt.Errorf("get users: %w", err)
	}

	s.Mu.Lock()
	for _, user := range users {
		s.Users[user.ID] = user.Name
	}
	s.Mu.Unlock()

	if err := s.hydrateFavorites(users); err != nil {
		return fmt.Errorf("hydrate favorites: %w", err)
	}

	if err := s.reconcileCollections(ctx, users); err != nil {
		return fmt.Errorf("reconcile collections: %w", err)
	}

	return nil
}

func (s *StateManager) logf(format string, args ...any) {
	if s.Config != nil && s.Config.Debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

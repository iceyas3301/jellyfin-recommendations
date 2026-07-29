package recommender

import (
	"fmt"
	"log"
)

func (s *StateManager) hydrateFavorites(users []user) error {
	newFavorites := make(map[string]map[string]bool)

	for _, user := range users {
		if s.Config.IsUserExcluded(user.ID, user.Name) {
			s.logf("Skipping excluded user %s (%s)", user.Name, user.ID)
			continue
		}

		favorites, err := s.getUserFavorites(user.ID)
		if err != nil {
			log.Printf("Warning: failed to fetch favorites for %s (%s): %v", user.Name, user.ID, err)
			continue
		}

		for _, item := range favorites {
			if newFavorites[item.ID] == nil {
				newFavorites[item.ID] = make(map[string]bool)
			}
			newFavorites[item.ID][user.ID] = true
		}
	}

	// Merge into existing state (preserves WS-added items)
	s.Mu.Lock()
	defer s.Mu.Unlock()

	// Add/update from poll
	for itemID, userSet := range newFavorites {
		if s.UserFavorites[itemID] == nil {
			s.UserFavorites[itemID] = make(map[string]bool)
		}
		for userID := range userSet {
			s.UserFavorites[itemID][userID] = true
		}
	}

	// Remove users that no longer have this item favorited
	for itemID, userSet := range s.UserFavorites {
		for userID := range userSet {
			if _, userHasItem := newFavorites[itemID]; !userHasItem {
				delete(userSet, userID)
			}
		}
		if len(userSet) == 0 {
			delete(s.UserFavorites, itemID)
		}
	}

	return nil
}

func (s *StateManager) getUsers() (users []user, err error) {
	return getJellyfin[[]user](s, "/Users")
}

func (s *StateManager) getUserFavorites(userID string) ([]baseItem, error) {
	if userID == "" {
		return nil, fmt.Errorf("empty userID")
	}
	endpoint := fmt.Sprintf("/Users/%s/Items?filters=IsFavorite&recursive=true", userID)
	resp, err := getJellyfin[queryUserFavoritesResponse](s, endpoint)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (s *StateManager) getUserNameByID(userID string) string {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return s.Users[userID]
}

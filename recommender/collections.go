package recommender

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

func (s *StateManager) reconcileCollections(ctx context.Context, users []user) error {
	existingCollections, err := s.getExistingCollections()
	if err != nil {
		return fmt.Errorf("query existing collections: %w", err)
	}

	// Snapshot under lock, then release before HTTP calls
	s.Mu.RLock()
	currentFavorites := make(map[string]map[string]bool)
	for k, v := range s.UserFavorites {
		currentFavorites[k] = v
	}
	s.Mu.RUnlock()

	type userPlan struct {
		user          user
		prefixedName  string
		expectedID    string
		expectedItems map[string]bool
		hasFavorites  bool
	}

	plans := make([]userPlan, 0, len(users))
	for _, user := range users {
		if s.Config.IsUserExcluded(user.ID, user.Name) {
			s.logf("Reconcile: skipping excluded user %s", user.Name)
			continue
		}
		prefixedName := s.Config.PrefixedName(user.Name)
		expectedItems := make(map[string]bool)
		for itemID, userMap := range currentFavorites {
			if userMap[user.ID] {
				expectedItems[itemID] = true
			}
		}
		log.Printf("Reconcile plan: user=%s (%s), prefixed=%q, existing=%q, favorites=%d",
			user.Name, user.ID, prefixedName, existingCollections[prefixedName], len(expectedItems))
		plans = append(plans, userPlan{
			user:          user,
			prefixedName:  prefixedName,
			expectedID:    existingCollections[prefixedName],
			expectedItems: expectedItems,
			hasFavorites:  len(expectedItems) > 0,
		})
	}

	for _, plan := range plans {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !plan.hasFavorites {
			if plan.expectedID != "" {
				if s.Config.DryRun {
					log.Printf("[DRY RUN] Would delete empty collection %q (ID: %s)", plan.prefixedName, plan.expectedID)
				} else {
					if err := s.deleteJellyfin(fmt.Sprintf("/Items/%s", plan.expectedID)); err != nil {
						log.Printf("Failed to delete empty collection %q: %v", plan.prefixedName, err)
					} else {
						log.Printf("Deleted empty collection %q", plan.prefixedName)
					}
				}
			}
			continue
		}

		collectionID := plan.expectedID

		if collectionID == "" {
			var firstItem string
			for id := range plan.expectedItems {
				firstItem = id
				break
			}
			if s.Config.DryRun {
				log.Printf("[DRY RUN] Would create collection %q with initial item %s", plan.prefixedName, firstItem)
				continue
			}
			var createErr error
			collectionID, createErr = s.createCollectionWithImage(plan.prefixedName, plan.user.ID, firstItem)
			if createErr != nil {
				log.Printf("Failed to create collection %q: %v", plan.prefixedName, createErr)
				continue
			}
			log.Printf("Created collection %q (ID: %s)", plan.prefixedName, collectionID)
		}

		actualItems, err := s.getCollectionItemIDs(collectionID)
		if err != nil {
			log.Printf("Failed to read items for collection %q: %v", plan.prefixedName, err)
			actualItems = make(map[string]bool)
		}
		log.Printf("Collection %q: %d actual items, %d expected", plan.prefixedName, len(actualItems), len(plan.expectedItems))

		// Add missing items
		for itemID := range plan.expectedItems {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !actualItems[itemID] {
				if s.Config.DryRun {
					s.logf("[DRY RUN] Would add item %s to %q", itemID, plan.prefixedName)
				} else if err := s.addItemToCollection(collectionID, itemID); err != nil {
					log.Printf("Failed to add %s to %q: %v", itemID, plan.prefixedName, err)
				} else {
					actualItems[itemID] = true
				}
			}
		}

		// Remove stale items
		for actualID := range actualItems {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !plan.expectedItems[actualID] {
				if s.Config.DryRun {
					s.logf("[DRY RUN] Would remove item %s from %q", actualID, plan.prefixedName)
				} else if err := s.removeItemFromCollection(collectionID, actualID); err != nil {
					log.Printf("Failed to remove %s from %q: %v", actualID, plan.prefixedName, err)
				} else {
					delete(actualItems, actualID)
				}
			}
		}

		if !s.Config.DryRun {
			s.Mu.Lock()
			s.UserCollections[plan.user.ID] = &UserCollectionInfo{
				CollectionID: collectionID,
				ItemIDs:      actualItems,
			}
			s.Mu.Unlock()
		}
	}

	return nil
}

func (s *StateManager) getCollectionItemIDs(collectionID string) (map[string]bool, error) {
	endpoint := fmt.Sprintf("/Items?parentId=%s&recursive=true", collectionID)
	resp, err := getJellyfin[queryUserFavoritesResponse](s, endpoint)
	if err != nil {
		return nil, err
	}
	itemSet := make(map[string]bool)
	for _, item := range resp.Items {
		itemSet[item.ID] = true
	}
	return itemSet, nil
}

func (s *StateManager) getExistingCollections() (map[string]string, error) {
	endpoint := "/Items?includeItemTypes=BoxSet&recursive=true"
	resp, err := getJellyfin[existingCollectionsResponse](s, endpoint)
	if err != nil {
		return nil, err
	}
	collections := make(map[string]string)
	for _, item := range resp.Items {
		collections[item.Name] = item.ID
	}
	return collections, nil
}

func (s *StateManager) createCollectionWithImage(name, userID, initialItemID string) (string, error) {
	newID, err := s.createCollection(name, initialItemID)
	if err != nil {
		return "", err
	}
	// Use initialItemID poster as the collection image
	imageBytes, contentType, err := s.getItemPrimaryImage(initialItemID)
	if err != nil {
		log.Printf("Warning: poster for %s: %v", initialItemID, err)
		return newID, nil
	}
	if err := s.setCollectionImage(newID, imageBytes, contentType); err != nil {
		log.Printf("Warning: failed to set collection image for %s: %v", name, err)
	}
	return newID, nil
}

func (s *StateManager) createCollection(name, initialItemID string) (string, error) {
	if name == "" || initialItemID == "" {
		return "", fmt.Errorf("name and initialItemID cannot be empty")
	}
	params := url.Values{}
	params.Set("Name", name)
	params.Set("Ids", initialItemID)
	endpoint := fmt.Sprintf("/Collections?%s", params.Encode())
	resp, err := postJellyfin[createCollectionResponse](s, endpoint, nil)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (s *StateManager) addItemToCollection(collectionID, itemID string) error {
	if collectionID == "" || itemID == "" {
		return fmt.Errorf("collectionID and itemID cannot be empty")
	}
	params := url.Values{}
	params.Set("Ids", itemID)
	endpoint := fmt.Sprintf("/Collections/%s/Items?%s", collectionID, params.Encode())
	_, err := postJellyfin[any](s, endpoint, nil)
	return err
}

func (s *StateManager) removeItemFromCollection(collectionID, itemID string) error {
	if collectionID == "" || itemID == "" {
		return fmt.Errorf("collectionID and itemID cannot be empty")
	}
	endpoint := fmt.Sprintf("/Collections/%s/Items?Ids=%s", collectionID, itemID)
	return s.deleteJellyfin(endpoint)
}

func (s *StateManager) getItemPrimaryImage(itemID string) ([]byte, string, error) {
	reqURL := fmt.Sprintf("%s/Items/%s/Images/Primary", s.Config.ServerURL, itemID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", authHeader(s.Config.APIKey))

	resp, err := s.newHTTPClient().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("poster HTTP %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}

	data, err := io.ReadAll(resp.Body)
	return data, ct, err
}

func (s *StateManager) setCollectionImage(itemID string, imageData []byte, contentType string) error {
	// Jellyfin expects base64-encoded image body
	b64Data := make([]byte, base64.StdEncoding.EncodedLen(len(imageData)))
	base64.StdEncoding.Encode(b64Data, imageData)

	reqURL := fmt.Sprintf("%s/Items/%s/Images/Primary", s.Config.ServerURL, itemID)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(b64Data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader(s.Config.APIKey))
	req.Header.Set("Content-Type", contentType)

	resp, err := s.newHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST image: HTTP %d", resp.StatusCode)
	}
	return nil
}

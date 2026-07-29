package recommender

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// StartWebSocketListener connects to Jellyfin's WebSocket and streams favorite updates.
// It reconnects automatically with exponential backoff on disconnect.
func (s *StateManager) StartWebSocketListener(ctx context.Context) {
	wsURL := strings.Replace(s.Config.ServerURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	fullURL := fmt.Sprintf("%s/socket", wsURL)

	backoff := time.Second
	maxBackoff := s.Config.WSReconnectMax

	for {
		if ctx.Err() != nil {
			log.Println("WebSocket listener shutting down (context cancelled)")
			return
		}

		log.Printf("Connecting to Jellyfin WebSocket at %s", s.Config.ServerURL)

		dialer := websocket.DefaultDialer
		header := http.Header{}
		header.Set("Authorization", authHeader(s.Config.APIKey))

		conn, _, err := dialer.Dial(fullURL, header)
		if err != nil {
			log.Printf("WebSocket connection failed: %v. Retrying in %v", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Reset backoff on successful connection
		backoff = time.Second
		log.Println("Connected to Jellyfin WebSocket")

		// Subscribe to UserDataChanged events (favorites, watched, etc.)
		subMsg := `{"MessageType":"UserDataChanged","Data":"0,1000"}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(subMsg)); err != nil {
			log.Printf("Failed to send subscription: %v", err)
			conn.Close()
			continue
		}
		log.Println("Subscribed to UserDataChanged events")

		// Read loop with context cancellation
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, messageBytes, err := conn.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					conn.Close()
					return
				}

				var msg WSMessage
				if err := json.Unmarshal(messageBytes, &msg); err != nil {
					log.Printf("WS: unparsed message: %s", string(messageBytes))
					continue
				}

				log.Printf("WS: received %s", msg.MessageType)
				if msg.MessageType == "UserDataChanged" {
					s.handleUserDataChanged(msg.Data)
				}
			}
		}()

		// Wait for either context cancellation or connection close
		select {
		case <-ctx.Done():
			conn.Close()
			<-done
			return
		case <-done:
			// Connection closed, will reconnect
		}

		// Backoff before reconnect
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (s *StateManager) handleUserDataChanged(data UserDataChanged) {
	for _, change := range data.UserDataList {
		userID := change.UserID
		itemID := change.ItemID
		isFav := change.Data.IsFavorite

		userName := s.getUserNameByID(userID)
		if userName == "" {
			s.logf("WS: unknown user %s, skipping", userID)
			continue
		}

		if s.Config.IsUserExcluded(userID, userName) {
			s.logf("WS: skipping excluded user %s", userName)
			continue
		}

		if isFav {
			log.Printf("WS: %s favorited %s", userName, itemID)
			if err := s.addUserFavorite(userID, userName, itemID); err != nil {
				log.Printf("WS: error adding favorite for %s: %v", userName, err)
			}
		} else {
			log.Printf("WS: %s unfavorited %s", userName, itemID)
			if err := s.removeUserFavorite(userID, itemID); err != nil {
				log.Printf("WS: error removing favorite for %s: %v", userName, err)
			}
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

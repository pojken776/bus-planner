package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// UserPreferences holds a user's location choices.
// Locations can be addresses, site names, or coordinates.
type UserPreferences struct {
	HomeLocation  string `json:"homeLocation"`
	WorkLocation  string `json:"workLocation"`
	RoutePriority string `json:"routePriority,omitempty"` // leastinterchange, leasttime, or leastwalking
}

// UserStore manages user preferences in memory and optionally persists to a JSON file.
type UserStore struct {
	mu    sync.RWMutex
	prefs map[int64]*UserPreferences // map of userID -> preferences
	file  string                     // path to persistence file (optional)
}

// NewUserStore creates a new in-memory user store.
// If filePath is not empty, it will load existing prefs from file and auto-save changes.
func NewUserStore(filePath string) *UserStore {
	store := &UserStore{
		prefs: make(map[int64]*UserPreferences),
		file:  filePath,
	}

	// Load from file if it exists.
	if filePath != "" {
		// Ensure directory exists
		dir := filepath.Dir(filePath)
		if dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}

		// If file doesn't exist, create an empty JSON file
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			_ = os.WriteFile(filePath, []byte("{}\n"), 0644)
		}

		_ = store.loadFromFile()
	}

	return store
}

// GetPrefs retrieves a user's preferences (or empty if not set).
func (s *UserStore) GetPrefs(userID int64) UserPreferences {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if prefs, exists := s.prefs[userID]; exists {
		return *prefs
	}
	return UserPreferences{}
}

// SetHome sets a user's home location.
func (s *UserStore) SetHome(userID int64, location string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prefs[userID]; !exists {
		s.prefs[userID] = &UserPreferences{}
	}
	s.prefs[userID].HomeLocation = location

	return s.saveToFile()
}

// SetWork sets a user's work location.
func (s *UserStore) SetWork(userID int64, location string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prefs[userID]; !exists {
		s.prefs[userID] = &UserPreferences{}
	}
	s.prefs[userID].WorkLocation = location

	return s.saveToFile()
}

// SetPriority sets a user's route priority preference.
func (s *UserStore) SetPriority(userID int64, priority string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prefs[userID]; !exists {
		s.prefs[userID] = &UserPreferences{}
	}
	s.prefs[userID].RoutePriority = priority

	return s.saveToFile()
}

// loadFromFile loads preferences from a JSON file.
func (s *UserStore) loadFromFile() error {
	if s.file == "" {
		return nil
	}

	data, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's ok.
		}
		return fmt.Errorf("read prefs file: %w", err)
	}

	var prefs map[string]*UserPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return fmt.Errorf("unmarshal prefs: %w", err)
	}

	// Convert string keys to int64 userIDs.
	for keyStr, userPrefs := range prefs {
		var userID int64
		if _, err := fmt.Sscanf(keyStr, "%d", &userID); err != nil {
			continue // Skip invalid keys.
		}
		s.prefs[userID] = userPrefs
	}

	return nil
}

// saveToFile persists preferences to a JSON file.
func (s *UserStore) saveToFile() error {
	if s.file == "" {
		return nil
	}

	// Ensure directory exists before writing file.
	dir := filepath.Dir(s.file)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ensure prefs dir: %w", err)
		}
	}

	// Convert int64 keys to string for JSON.
	prefs := make(map[string]*UserPreferences)
	for userID, userPrefs := range s.prefs {
		prefs[fmt.Sprintf("%d", userID)] = userPrefs
	}

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prefs: %w", err)
	}

	if err := os.WriteFile(s.file, data, 0644); err != nil {
		return fmt.Errorf("write prefs file: %w", err)
	}

	return nil
}

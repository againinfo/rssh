package data

import (
	"encoding/json"
	"strings"
	"time"
)

// KnownClient persists last-seen client info keyed by public key fingerprint.
// This allows the UI to keep showing offline machines across server restarts.
type KnownClient struct {
	// Fingerprint is the stable unique identifier (server-side pubkey fingerprint).
	Fingerprint string `gorm:"primaryKey;size:128"`

	Hostname   string
	RemoteAddr string
	Owners     string
	Comment    string
	Version    string

	// AliasesJSON stores []string as JSON for search convenience.
	AliasesJSON string `gorm:"type:text"`

	// LastSeen is updated on connect/disconnect.
	LastSeen time.Time

	// Status is last observed status ("connected"|"disconnected").
	Status string
}

func (KnownClient) TableName() string { return "known_clients" }

func (kc KnownClient) Aliases() []string {
	var out []string
	_ = json.Unmarshal([]byte(kc.AliasesJSON), &out)
	return out
}

func MarshalAliases(aliases []string) string {
	// Keep it compact and resilient.
	clean := make([]string, 0, len(aliases))
	seen := map[string]bool{}
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		clean = append(clean, a)
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

func UpsertKnownClient(kc KnownClient) error {
	if db == nil {
		return nil
	}
	kc.Fingerprint = strings.TrimSpace(kc.Fingerprint)
	if kc.Fingerprint == "" {
		return nil
	}
	if kc.LastSeen.IsZero() {
		kc.LastSeen = time.Now()
	}
	if strings.TrimSpace(kc.Status) == "" {
		kc.Status = "disconnected"
	}
	return db.Save(&kc).Error
}

func ListKnownClients() ([]KnownClient, error) {
	if db == nil {
		return []KnownClient{}, nil
	}
	var out []KnownClient
	if err := db.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func DeleteKnownClient(fingerprint string) error {
	if db == nil {
		return nil
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return nil
	}
	return db.Delete(&KnownClient{Fingerprint: fp}).Error
}

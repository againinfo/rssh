package data

import (
	"errors"
	"strings"
	"time"
)

// ClientCommSettings stores connectivity/heartbeat options for a client keyed by fingerprint.
type ClientCommSettings struct {
	Fingerprint string `gorm:"primaryKey;size:128" json:"fingerprint"`
	// ServerTimeoutSeconds controls the value sent via keepalive-rssh@golang.org payload.
	// 0 means "do not override".
	ServerTimeoutSeconds int       `json:"server_timeout_seconds"`
	ClientHeartbeatSec   int       `json:"client_heartbeat_sec"`
	SleepWindow          string    `gorm:"size:32" json:"sleep_window"` // "HH:MM-HH:MM"
	SleepCheckSec        int       `json:"sleep_check_sec"`
	SleepUntil           string    `gorm:"size:40" json:"sleep_until"` // RFC3339
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func GetClientCommSettings(fingerprint string) (ClientCommSettings, bool, error) {
	if db == nil {
		return ClientCommSettings{}, false, errors.New("database not initialized")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return ClientCommSettings{}, false, errors.New("fingerprint is empty")
	}
	var m ClientCommSettings
	tx := db.Limit(1).Find(&m, "fingerprint = ?", fp)
	if tx.Error != nil {
		return ClientCommSettings{}, false, tx.Error
	}
	if tx.RowsAffected == 0 {
		return ClientCommSettings{Fingerprint: fp}, false, nil
	}
	return m, true, nil
}

func UpsertClientCommSettings(fingerprint string, s ClientCommSettings) (ClientCommSettings, error) {
	if db == nil {
		return ClientCommSettings{}, errors.New("database not initialized")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return ClientCommSettings{}, errors.New("fingerprint is empty")
	}
	s.Fingerprint = fp

	var existing ClientCommSettings
	tx := db.Limit(1).Find(&existing, "fingerprint = ?", fp)
	if tx.Error != nil {
		return ClientCommSettings{}, tx.Error
	}
	if tx.RowsAffected == 0 {
		return s, db.Create(&s).Error
	}
	existing.ServerTimeoutSeconds = s.ServerTimeoutSeconds
	existing.ClientHeartbeatSec = s.ClientHeartbeatSec
	existing.SleepWindow = strings.TrimSpace(s.SleepWindow)
	existing.SleepCheckSec = s.SleepCheckSec
	existing.SleepUntil = strings.TrimSpace(s.SleepUntil)
	return existing, db.Save(&existing).Error
}

func DeleteClientCommSettings(fingerprint string) error {
	if db == nil {
		return nil
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return nil
	}
	return db.Delete(&ClientCommSettings{Fingerprint: fp}).Error
}

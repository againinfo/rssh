package data

import (
	"errors"
	"strings"
	"time"
)

// ClientMeta stores user-defined annotations for a client.
//
// We key this by the client's public key fingerprint (stable across reconnects),
// not by the ephemeral connection ID.
type ClientMeta struct {
	Fingerprint string    `gorm:"primaryKey;size:128" json:"fingerprint"`
	Group       string    `gorm:"size:128" json:"group"`
	Note        string    `gorm:"size:2048" json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func GetClientMeta(fingerprint string) (ClientMeta, bool, error) {
	if db == nil {
		return ClientMeta{}, false, errors.New("database not initialized")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return ClientMeta{}, false, errors.New("fingerprint is empty")
	}

	var m ClientMeta
	tx := db.Limit(1).Find(&m, "fingerprint = ?", fp)
	if tx.Error != nil {
		return ClientMeta{}, false, tx.Error
	}
	if tx.RowsAffected == 0 {
		return ClientMeta{Fingerprint: fp}, false, nil
	}
	return m, true, nil
}

func UpsertClientMeta(fingerprint, group, note string) (ClientMeta, error) {
	if db == nil {
		return ClientMeta{}, errors.New("database not initialized")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return ClientMeta{}, errors.New("fingerprint is empty")
	}

	group = strings.TrimSpace(group)
	note = strings.TrimSpace(note)

	var m ClientMeta
	tx := db.Limit(1).Find(&m, "fingerprint = ?", fp)
	if tx.Error != nil {
		return ClientMeta{}, tx.Error
	}
	if tx.RowsAffected == 0 {
		m = ClientMeta{Fingerprint: fp, Group: group, Note: note}
		return m, db.Create(&m).Error
	}

	m.Group = group
	m.Note = note
	return m, db.Save(&m).Error
}

// PatchClientMeta updates only specified fields (non-nil pointers).
// If the record doesn't exist it is created with missing fields as empty strings.
func PatchClientMeta(fingerprint string, group *string, note *string) (ClientMeta, error) {
	if db == nil {
		return ClientMeta{}, errors.New("database not initialized")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return ClientMeta{}, errors.New("fingerprint is empty")
	}
	if group == nil && note == nil {
		return ClientMeta{}, errors.New("no fields to update")
	}

	var m ClientMeta
	tx := db.Limit(1).Find(&m, "fingerprint = ?", fp)
	if tx.Error != nil {
		return ClientMeta{}, tx.Error
	}
	if tx.RowsAffected == 0 {
		m = ClientMeta{Fingerprint: fp}
		if group != nil {
			m.Group = strings.TrimSpace(*group)
		}
		if note != nil {
			m.Note = strings.TrimSpace(*note)
		}
		return m, db.Create(&m).Error
	}

	if group != nil {
		m.Group = strings.TrimSpace(*group)
	}
	if note != nil {
		m.Note = strings.TrimSpace(*note)
	}
	return m, db.Save(&m).Error
}

func ListClientMetas(fingerprints []string) (map[string]ClientMeta, error) {
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	uniq := make([]string, 0, len(fingerprints))
	seen := make(map[string]struct{}, len(fingerprints))
	for _, fp := range fingerprints {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		uniq = append(uniq, fp)
	}
	out := make(map[string]ClientMeta, len(uniq))
	if len(uniq) == 0 {
		return out, nil
	}

	var rows []ClientMeta
	if err := db.Where("fingerprint IN ?", uniq).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Fingerprint] = r
	}
	return out, nil
}

func DeleteClientMeta(fingerprint string) error {
	if db == nil {
		return nil
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return nil
	}
	return db.Delete(&ClientMeta{Fingerprint: fp}).Error
}

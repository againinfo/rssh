package data

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type FileCache struct {
	Key         string `gorm:"primaryKey;size:64"`
	Fingerprint string `gorm:"index;size:128"`
	Path        string `gorm:"index;size:1024"`
	Offset      int64
	MaxBytes    int64
	Size        int64
	ModUnixNano int64
	Encoding    string `gorm:"size:32"`
	Content     string `gorm:"type:text"`
	Truncated   bool
	Mode        string    `gorm:"size:64"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type FileCacheMeta struct {
	Size        int64
	Mode        string
	ModUnixNano int64
}

func FileCacheKey(fingerprint, path string, offset, maxBytes, size, modUnixNano int64) string {
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d",
		strings.TrimSpace(fingerprint),
		strings.TrimSpace(path),
		offset,
		maxBytes,
		size,
		modUnixNano,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func GetFileCache(key string) (FileCache, bool, error) {
	if db == nil {
		return FileCache{}, false, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return FileCache{}, false, errors.New("cache key is empty")
	}
	var cache FileCache
	tx := db.Limit(1).Find(&cache, "key = ?", key)
	if tx.Error != nil {
		return FileCache{}, false, tx.Error
	}
	return cache, tx.RowsAffected > 0, nil
}

func UpsertFileCache(cache FileCache) error {
	if db == nil {
		return nil
	}
	cache.Key = strings.TrimSpace(cache.Key)
	cache.Fingerprint = strings.TrimSpace(cache.Fingerprint)
	cache.Path = strings.TrimSpace(cache.Path)
	if cache.Key == "" {
		return errors.New("cache key is empty")
	}
	if cache.Fingerprint == "" || cache.Path == "" {
		return errors.New("cache fingerprint/path is empty")
	}

	var existing FileCache
	tx := db.Limit(1).Find(&existing, "key = ?", cache.Key)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return db.Create(&cache).Error
	}
	cache.CreatedAt = existing.CreatedAt
	return db.Save(&cache).Error
}

func DeleteFileCacheForPath(fingerprint, path string) error {
	if db == nil {
		return nil
	}
	fingerprint = strings.TrimSpace(fingerprint)
	path = strings.TrimSpace(path)
	if fingerprint == "" || path == "" {
		return nil
	}
	return db.Where("fingerprint = ? AND path = ?", fingerprint, path).Delete(&FileCache{}).Error
}

func DeleteFileCacheForFingerprint(fingerprint string) error {
	if db == nil {
		return nil
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return nil
	}
	return db.Where("fingerprint = ?", fingerprint).Delete(&FileCache{}).Error
}

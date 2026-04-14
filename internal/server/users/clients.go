package users

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"rssh/internal"
	"rssh/internal/server/data"
	"rssh/pkg/trie"
)

var (
	allClients = map[string]*ssh.ServerConn{}

	ownedByAll = map[string]*ssh.ServerConn{}

	uniqueIdToAllAliases = map[string][]string{}

	// alias to uniqueID
	aliases = map[string]map[string]bool{}

	usernameRegex = regexp.MustCompile(`[^\w-]`)

	globalAutoComplete = trie.NewTrie()

	PublicClientsAutoComplete = trie.NewTrie()

	// knownClients tracks last-seen client info keyed by fingerprint so the UI can
	// keep showing offline machines.
	knownClients = map[string]ClientSummary{} // fingerprint -> summary

	// fingerprintToConn tracks the currently connected session for a fingerprint.
	fingerprintToConn = map[string]*ssh.ServerConn{} // fingerprint -> conn
)

type ClientSummary struct {
	// ID is the stable UI identifier (fingerprint).
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"` // connected|disconnected
	LastSeen  time.Time `json:"last_seen"`

	Hostname    string   `json:"hostname"`
	RemoteAddr  string   `json:"remote_addr"`
	Owners      string   `json:"owners"`
	Fingerprint string   `json:"fingerprint"`
	Comment     string   `json:"comment"`
	Version     string   `json:"version"`
	Aliases     []string `json:"aliases"`
}

func NormaliseHostname(hostname string) string {
	hostname = strings.ToLower(hostname)

	hostname = usernameRegex.ReplaceAllString(hostname, ".")

	return hostname
}

func summaryFromConn(id string, conn *ssh.ServerConn) ClientSummary {
	aliasesCopy := append([]string{}, uniqueIdToAllAliases[id]...)
	sort.Strings(aliasesCopy)

	fp := strings.TrimSpace(conn.Permissions.Extensions["pubkey-fp"])
	if fp == "" {
		fp = "unknown"
	}

	return ClientSummary{
		ID:          id,
		SessionID:   id,
		Status:      "connected",
		LastSeen:    time.Now(),
		Hostname:    NormaliseHostname(conn.User()),
		RemoteAddr:  conn.RemoteAddr().String(),
		Owners:      conn.Permissions.Extensions["owners"],
		Fingerprint: fp,
		Comment:     conn.Permissions.Extensions["comment"],
		Version:     string(conn.ClientVersion()),
		Aliases:     aliasesCopy,
	}
}

// GetClientSummary returns the last known summary for a fingerprint (stable UI id).
func GetClientSummary(fingerprint string) (ClientSummary, bool) {
	lck.RLock()
	defer lck.RUnlock()

	s, ok := knownClients[fingerprint]
	if !ok {
		return ClientSummary{}, false
	}
	return s, true
}

// ListAllClients returns known controllable RSSH clients visible on the server (connected + offline).
// filter uses glob matching (prefix*).
func ListAllClients(filter string) ([]ClientSummary, error) {
	filter = strings.TrimSpace(filter)
	if filter != "" {
		filter = filter + "*"
		if _, err := filepath.Match(filter, ""); err != nil {
			return nil, fmt.Errorf("filter is not well formed")
		}
	}

	lck.RLock()
	defer lck.RUnlock()

	out := make([]ClientSummary, 0, len(knownClients))
	needle := strings.ToLower(strings.TrimSuffix(filter, "*"))
	for _, s := range knownClients {
		if filter != "" {
			hay := strings.ToLower(strings.Join([]string{
				s.ID,
				s.SessionID,
				s.Status,
				s.Hostname,
				s.RemoteAddr,
				s.Owners,
				s.Fingerprint,
				s.Comment,
				s.Version,
				strings.Join(s.Aliases, " "),
			}, " "))
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListConnectedClients returns only currently connected controllable RSSH clients (session IDs).
// This is used by pages/actions that require an active control channel (e.g. forwards).
func ListConnectedClients(filter string) ([]ClientSummary, error) {
	filter = strings.TrimSpace(filter)
	if filter != "" {
		filter = filter + "*"
		if _, err := filepath.Match(filter, ""); err != nil {
			return nil, fmt.Errorf("filter is not well formed")
		}
	}

	lck.RLock()
	defer lck.RUnlock()

	out := make([]ClientSummary, 0, len(allClients))
	for id, conn := range allClients {
		if filter != "" && !_matches(filter, id, conn.RemoteAddr().String()) {
			continue
		}
		out = append(out, summaryFromConn(id, conn))
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LoadKnownClientsFromDB preloads the known-clients registry from data.db.
// It marks all persisted records as offline on load (connections are in-memory).
func LoadKnownClientsFromDB() error {
	recs, err := data.ListKnownClients()
	if err != nil {
		return err
	}

	lck.Lock()
	defer lck.Unlock()

	for _, r := range recs {
		fp := strings.TrimSpace(r.Fingerprint)
		if fp == "" {
			continue
		}
		var aliases []string
		if strings.TrimSpace(r.AliasesJSON) != "" {
			_ = json.Unmarshal([]byte(r.AliasesJSON), &aliases)
		}
		knownClients[fp] = ClientSummary{
			ID:          fp,
			SessionID:   "",
			Status:      "disconnected",
			LastSeen:    r.LastSeen,
			Hostname:    strings.TrimSpace(r.Hostname),
			RemoteAddr:  strings.TrimSpace(r.RemoteAddr),
			Owners:      strings.TrimSpace(r.Owners),
			Fingerprint: fp,
			Comment:     strings.TrimSpace(r.Comment),
			Version:     strings.TrimSpace(r.Version),
			Aliases:     aliases,
		}
	}

	return nil
}

// GetClientConn returns the connected client by unique ID.
func GetClientConn(id string) (*ssh.ServerConn, bool) {
	lck.RLock()
	defer lck.RUnlock()
	c, ok := allClients[id]
	return c, ok
}

func GetClientConnByFingerprint(fingerprint string) (*ssh.ServerConn, bool) {
	lck.RLock()
	defer lck.RUnlock()
	c, ok := fingerprintToConn[fingerprint]
	return c, ok
}

// SetClientOwnership changes ownership for a connected client without user privilege checks.
// Note: ownership changes are in-memory only (same as CLI `access` command).
func SetClientOwnership(uniqueID, newOwners string) error {
	lck.Lock()
	defer lck.Unlock()

	sc, ok := allClients[uniqueID]
	if !ok {
		if sc, ok = ownedByAll[uniqueID]; !ok {
			return fmt.Errorf("not found")
		}
	}

	_disassociateFromOwners(uniqueID, sc.Permissions.Extensions["owners"])
	_associateToOwners(uniqueID, newOwners, sc)
	sc.Permissions.Extensions["owners"] = newOwners
	return nil
}

func SetClientOwnershipByFingerprint(fingerprint, newOwners string) error {
	lck.RLock()
	s, ok := knownClients[fingerprint]
	lck.RUnlock()
	if !ok || strings.TrimSpace(s.SessionID) == "" {
		return fmt.Errorf("not connected")
	}
	if err := SetClientOwnership(s.SessionID, newOwners); err != nil {
		return err
	}

	// Persist owner update for offline display.
	lck.Lock()
	if cur, ok := knownClients[fingerprint]; ok {
		cur.Owners = newOwners
		cur.LastSeen = time.Now()
		knownClients[fingerprint] = cur
		_ = data.UpsertKnownClient(data.KnownClient{
			Fingerprint: cur.Fingerprint,
			Hostname:    cur.Hostname,
			RemoteAddr:  cur.RemoteAddr,
			Owners:      cur.Owners,
			Comment:     cur.Comment,
			Version:     cur.Version,
			AliasesJSON: data.MarshalAliases(cur.Aliases),
			LastSeen:    cur.LastSeen,
			Status:      cur.Status,
		})
	}
	lck.Unlock()

	return nil
}

// SearchAllClientConns returns all connected clients matching filter (glob prefix match),
// across public and private ownership sets.
func SearchAllClientConns(filter string) (map[string]*ssh.ServerConn, error) {
	filter = strings.TrimSpace(filter)
	if filter != "" {
		filter = filter + "*"
		if _, err := filepath.Match(filter, ""); err != nil {
			return nil, fmt.Errorf("filter is not well formed")
		}
	}

	lck.RLock()
	defer lck.RUnlock()

	out := make(map[string]*ssh.ServerConn)
	for id, conn := range allClients {
		if filter != "" && !_matches(filter, id, conn.RemoteAddr().String()) {
			continue
		}
		out[id] = conn
	}
	return out, nil
}

func AssociateClient(conn *ssh.ServerConn) (string, string, error) {
	lck.Lock()
	defer lck.Unlock()

	idString, err := internal.RandomString(20)
	if err != nil {
		return "", "", err
	}

	username := NormaliseHostname(conn.User())
	fp := strings.TrimSpace(conn.Permissions.Extensions["pubkey-fp"])
	if fp == "" {
		fp = "unknown"
	}

	addAlias(idString, username)
	addAlias(idString, conn.RemoteAddr().String())
	addAlias(idString, conn.Permissions.Extensions["pubkey-fp"])
	if conn.Permissions.Extensions["comment"] != "" {
		addAlias(idString, conn.Permissions.Extensions["comment"])
	}
	allClients[idString] = conn

	globalAutoComplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
	if conn.Permissions.Extensions["comment"] != "" {
		globalAutoComplete.Add(conn.Permissions.Extensions["comment"])
	}

	_associateToOwners(idString, conn.Permissions.Extensions["owners"], conn)

	// Stable UI registry (keyed by fingerprint)
	s := summaryFromConn(idString, conn)
	s.ID = fp
	s.Fingerprint = fp
	s.Status = "connected"
	s.SessionID = idString
	s.LastSeen = time.Now()
	s.Aliases = append([]string{username, conn.RemoteAddr().String(), fp}, s.Aliases...)
	if conn.Permissions.Extensions["comment"] != "" {
		s.Aliases = append(s.Aliases, conn.Permissions.Extensions["comment"])
	}
	knownClients[fp] = s
	fingerprintToConn[fp] = conn
	_ = data.UpsertKnownClient(data.KnownClient{
		Fingerprint: fp,
		Hostname:    s.Hostname,
		RemoteAddr:  s.RemoteAddr,
		Owners:      s.Owners,
		Comment:     s.Comment,
		Version:     s.Version,
		AliasesJSON: data.MarshalAliases(s.Aliases),
		LastSeen:    s.LastSeen,
		Status:      s.Status,
	})

	return idString, username, nil

}

func _associateToOwners(idString, owners string, conn *ssh.ServerConn) {
	username := NormaliseHostname(conn.User())
	ownersParts := strings.Split(owners, ",")

	if len(ownersParts) == 1 && ownersParts[0] == "" {
		// Owners is empty, so add it to the public list
		ownedByAll[idString] = conn

		PublicClientsAutoComplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
		if conn.Permissions.Extensions["comment"] != "" {
			PublicClientsAutoComplete.Add(conn.Permissions.Extensions["comment"])
		}

	} else {
		for _, owner := range ownersParts {
			// Cant error if we're not adding a connection
			u, _, _ := _createOrGetUser(owner, nil)
			u.clients[idString] = conn

			u.autocomplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
			if conn.Permissions.Extensions["comment"] != "" {
				u.autocomplete.Add(conn.Permissions.Extensions["comment"])
			}
		}
	}

}

func addAlias(uniqueId, newAlias string) {
	if _, ok := aliases[newAlias]; !ok {
		aliases[newAlias] = make(map[string]bool)
	}

	uniqueIdToAllAliases[uniqueId] = append(uniqueIdToAllAliases[uniqueId], newAlias)
	aliases[newAlias][uniqueId] = true
}

func DisassociateClient(uniqueId string, conn *ssh.ServerConn) {
	lck.Lock()
	defer lck.Unlock()

	if _, ok := allClients[uniqueId]; !ok {
		//If this is already removed then we dont need to remove it again.
		return
	}

	// Keep stable UI record and mark offline.
	fp := strings.TrimSpace(conn.Permissions.Extensions["pubkey-fp"])
	if fp != "" {
		s, ok := knownClients[fp]
		if !ok {
			s = summaryFromConn(uniqueId, conn)
			s.ID = fp
			s.Fingerprint = fp
		}
		s.Status = "disconnected"
		s.SessionID = ""
		s.LastSeen = time.Now()
		knownClients[fp] = s
		delete(fingerprintToConn, fp)
		_ = data.UpsertKnownClient(data.KnownClient{
			Fingerprint: fp,
			Hostname:    s.Hostname,
			RemoteAddr:  s.RemoteAddr,
			Owners:      s.Owners,
			Comment:     s.Comment,
			Version:     s.Version,
			AliasesJSON: data.MarshalAliases(s.Aliases),
			LastSeen:    s.LastSeen,
			Status:      s.Status,
		})
	}

	globalAutoComplete.Remove(uniqueId)
	currentAliases, ok := uniqueIdToAllAliases[uniqueId]
	if ok {
		// Remove the global references of the aliases and auto completes
		for _, alias := range currentAliases {
			if len(aliases[alias]) <= 1 {
				globalAutoComplete.Remove(alias)
				delete(aliases, alias)
			}

			delete(aliases[alias], uniqueId)
		}
	}

	_disassociateFromOwners(uniqueId, conn.Permissions.Extensions["owners"])

	delete(allClients, uniqueId)
	delete(uniqueIdToAllAliases, uniqueId)

}

// DeleteKnownClient removes an offline client from the in-memory registry and persistence.
// It only succeeds when the client is currently offline (no active control channel).
func DeleteKnownClient(fingerprint string) error {
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return fmt.Errorf("fingerprint is empty")
	}

	lck.Lock()
	defer lck.Unlock()

	s, ok := knownClients[fp]
	if !ok {
		return fmt.Errorf("not found")
	}
	if strings.TrimSpace(s.SessionID) != "" {
		return fmt.Errorf("client is connected")
	}
	delete(knownClients, fp)
	delete(fingerprintToConn, fp)
	_ = data.DeleteKnownClient(fp)
	_ = data.DeleteClientMeta(fp)
	_ = data.DeleteClientCommSettings(fp)
	return nil
}

func _disassociateFromOwners(uniqueId, owners string) {
	ownersParts := strings.Split(owners, ",")

	currentAliases := uniqueIdToAllAliases[uniqueId]

	if len(ownersParts) == 1 && ownersParts[0] == "" {
		delete(ownedByAll, uniqueId)

		PublicClientsAutoComplete.Remove(uniqueId)
		PublicClientsAutoComplete.RemoveMultiple(currentAliases...)

	} else {
		for _, owner := range ownersParts {

			u, err := _getUser(owner)
			if err != nil {
				continue
			}

			delete(u.clients, uniqueId)

			u.autocomplete.Remove(uniqueId)
			u.autocomplete.RemoveMultiple(currentAliases...)

			// If the owner has no clients after we do the delete, then remove the construct from memory
			if len(u.clients) == 0 {
				delete(users, owner)
			}
		}
	}
}

package ui

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
	"rssh/internal"
	"rssh/internal/server/data"
	"rssh/internal/server/multiplexer"
	"rssh/internal/server/observers"
	"rssh/internal/server/socksproxy"
	"rssh/internal/server/tunnels"
	"rssh/internal/server/users"
	"rssh/internal/server/webserver"
)

//go:embed templates/*.html assets/* assets/xterm/* images/*
var content embed.FS

type Config struct {
	Addr      string
	Username  string
	Password  string
	EnableSSE bool
}

type Server struct {
	cfg Config
	srv *http.Server
	tpl map[string]*template.Template
}

var tunnelManager = tunnels.NewManager()
var socksManager = socksproxy.NewManager()

func Start(cfg Config) error {
	if strings.TrimSpace(cfg.Addr) == "" {
		return errors.New("ui addr is empty")
	}
	if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(cfg.Password) == "" {
		return errors.New("ui basic auth missing username/password")
	}

	funcs := template.FuncMap{
		"since": func(t time.Time) string {
			d := time.Since(t).Round(time.Second)
			if d < 0 {
				d = 0
			}
			return d.String()
		},
	}

	// IMPORTANT: each page template defines a template named "content".
	// If we parse all pages into one template set, the last parsed "content" wins,
	// causing every page to render the same content. To avoid this, clone base per page.
	baseTpl, err := template.New("base").Funcs(funcs).ParseFS(content, "templates/base.html")
	if err != nil {
		return err
	}

	pages := []string{
		"templates/index.html",
		"templates/client.html",
		"templates/exec.html",
		"templates/client_forwards.html",
		"templates/downloads.html",
		"templates/build.html",
		"templates/forwards.html",
		"templates/webhooks.html",
		"templates/files.html",
		"templates/shell.html",
		"templates/error.html",
	}

	pageTpls := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		cloned, err := baseTpl.Clone()
		if err != nil {
			return err
		}
		if _, err := cloned.ParseFS(content, p); err != nil {
			return err
		}
		pageTpls[path.Base(p)] = cloned
	}

	s := &Server{
		cfg: cfg,
		tpl: pageTpls,
	}

	mux := http.NewServeMux()
	assetsFS, err := fs.Sub(content, "assets")
	if err != nil {
		return err
	}
	mux.Handle("GET /ui/assets/", http.StripPrefix("/ui/assets/", http.FileServer(http.FS(assetsFS))))
	imagesFS, err := fs.Sub(content, "images")
	if err != nil {
		return err
	}
	mux.Handle("GET /ui/images/", http.StripPrefix("/ui/images/", http.FileServer(http.FS(imagesFS))))
	mux.HandleFunc("GET /ui/", s.handleIndex)
	mux.HandleFunc("GET /ui/clients/", s.handleClient)
	mux.HandleFunc("GET /ui/downloads", s.handleDownloads)
	mux.HandleFunc("GET /ui/build", s.handleBuild)
	mux.HandleFunc("GET /ui/forwards", s.handleForwards)
	mux.HandleFunc("GET /ui/webhooks", s.handleWebhooks)
	mux.HandleFunc("GET /ui/api/clients", s.handleAPIClients)
	mux.HandleFunc("GET /ui/api/clients/", s.handleAPIClientActions)
	mux.HandleFunc("POST /ui/api/clients/", s.handleAPIClientActions)
	mux.HandleFunc("POST /ui/api/clientmeta", s.handleAPIClientMetaBulk)
	mux.HandleFunc("POST /ui/api/build", s.handleAPIBuild)
	mux.HandleFunc("POST /ui/api/downloads/", s.handleAPIDownloadActions)
	mux.HandleFunc("GET /ui/api/forwards/server", s.handleAPIServerForwards)
	mux.HandleFunc("POST /ui/api/forwards/server", s.handleAPIServerForwards)
	mux.HandleFunc("GET /ui/api/forwards/client/", s.handleAPIClientForwards)
	mux.HandleFunc("POST /ui/api/forwards/client/", s.handleAPIClientForwards)
	mux.HandleFunc("GET /ui/api/webhooks", s.handleAPIWebhooks)
	mux.HandleFunc("POST /ui/api/webhooks", s.handleAPIWebhooks)
	mux.HandleFunc("GET /ui/api/tunnels", s.handleAPITunnels)
	mux.HandleFunc("POST /ui/api/tunnels", s.handleAPITunnels)
	mux.HandleFunc("POST /ui/api/tunnels/", s.handleAPITunnelActions)
	mux.HandleFunc("GET /ui/api/socks", s.handleAPISOCKS)
	mux.HandleFunc("POST /ui/api/socks", s.handleAPISOCKS)
	mux.HandleFunc("POST /ui/api/socks/", s.handleAPISOCKSActions)
	mux.HandleFunc("GET /ui/events", s.handleEvents)
	mux.Handle("GET /ui/ws/clients/", websocket.Handler(s.handleShellWS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	handler := basicAuthMiddleware(mux, cfg.Username, cfg.Password)
	s.srv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	log.Printf("UI listening on http://%s/ui/ (basic auth enabled)\n", ln.Addr().String())
	return s.srv.Serve(ln)
}

func basicAuthMiddleware(next http.Handler, username, password string) http.Handler {
	realm := `Basic realm="reverse_ssh"`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", realm)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type pageBase struct {
	Title   string
	Version string
	Now     time.Time
}

type clientRow struct {
	ID          string
	Hostname    string
	RemoteAddr  string
	Owners      string
	KeyLabel    string
	Version     string
	OS          string
	Group       string
	Note        string
	Fingerprint string
	Status      string
}

var clientOSRe = regexp.MustCompile(`(?i)(?:^|[- ])([a-z0-9]+)_([a-z0-9]+)$`)

func parseClientOS(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	m := clientOSRe.FindStringSubmatch(version)
	if len(m) != 3 {
		return ""
	}
	goos := strings.ToLower(m[1])
	goarch := strings.ToLower(m[2])
	return goos + "/" + goarch
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui/" {
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	clients, err := users.ListAllClients(q)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, err)
		return
	}
	if q != "" {
		// Add clients that match group/note even when the core filter doesn't match.
		all, err2 := users.ListAllClients("")
		if err2 == nil {
			seen := make(map[string]struct{}, len(clients))
			for _, c := range clients {
				seen[c.ID] = struct{}{}
			}
			for _, c := range all {
				if _, ok := seen[c.ID]; ok {
					continue
				}
				// meta match checked later once we join metas; add for now.
				clients = append(clients, c)
				seen[c.ID] = struct{}{}
			}
		}
	}

	fps := make([]string, 0, len(clients))
	for _, c := range clients {
		fps = append(fps, c.Fingerprint)
	}

	metas, _ := data.ListClientMetas(fps)

	rows := make([]clientRow, 0, len(clients))
	for _, c := range clients {
		keyLabel := c.Fingerprint
		if c.Comment != "" {
			keyLabel = c.Comment
		}
		owners := c.Owners
		if owners == "" {
			owners = "public"
		}

		var group, note string
		if m, ok := metas[c.Fingerprint]; ok {
			group, note = m.Group, m.Note
		}

		rows = append(rows, clientRow{
			ID:          c.ID,
			Hostname:    c.Hostname,
			RemoteAddr:  c.RemoteAddr,
			Owners:      owners,
			KeyLabel:    keyLabel,
			Version:     c.Version,
			OS:          parseClientOS(c.Version),
			Group:       group,
			Note:        note,
			Fingerprint: c.Fingerprint,
			Status:      c.Status,
		})
	}

	// Search enhancement: if q is set, match across group/note/os/version/host/etc.
	if q != "" {
		qLower := strings.ToLower(q)
		filtered := rows[:0]
		for _, r := range rows {
			hay := strings.ToLower(strings.Join([]string{
				r.ID,
				r.Hostname,
				r.RemoteAddr,
				r.Owners,
				r.KeyLabel,
				r.Version,
				r.OS,
				r.Group,
				r.Note,
			}, " "))
			if strings.Contains(hay, qLower) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	sort.Slice(rows, func(i, j int) bool {
		gi := strings.ToLower(strings.TrimSpace(rows[i].Group))
		gj := strings.ToLower(strings.TrimSpace(rows[j].Group))
		if gi != gj {
			if gi == "" {
				return false
			}
			if gj == "" {
				return true
			}
			return gi < gj
		}
		if rows[i].Hostname == rows[j].Hostname {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Hostname < rows[j].Hostname
	})

	type groupBlock struct {
		Name string
		Rows []clientRow
		Open bool
	}
	grouped := map[string][]clientRow{}
	for _, r := range rows {
		g := strings.TrimSpace(r.Group)
		if g == "" {
			g = "(ungrouped)"
		}
		grouped[g] = append(grouped[g], r)
	}
	groupNames := make([]string, 0, len(grouped))
	for g := range grouped {
		groupNames = append(groupNames, g)
	}
	sort.Slice(groupNames, func(i, j int) bool {
		// Put ungrouped at the end.
		if groupNames[i] == "(ungrouped)" {
			return false
		}
		if groupNames[j] == "(ungrouped)" {
			return true
		}
		return strings.ToLower(groupNames[i]) < strings.ToLower(groupNames[j])
	})
	blocks := make([]groupBlock, 0, len(groupNames))
	for _, g := range groupNames {
		blocks = append(blocks, groupBlock{
			Name: g,
			Rows: grouped[g],
			Open: q != "" || g != "(ungrouped)",
		})
	}

	s.render(w, r, http.StatusOK, "index.html", map[string]any{
		"Base": pageBase{
			Title:   "Dashboard",
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Query":  q,
		"Groups": blocks,
		"Total":  len(rows),
		"Enable": s.cfg.EnableSSE,
	})
}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/ui/clients/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}

	parts := strings.Split(rest, "/")
	id := parts[0]
	if len(parts) >= 2 {
		switch parts[1] {
		case "files":
			s.handleFilesPage(w, r, id)
			return
		case "shell":
			s.handleShellPage(w, r, id)
			return
		case "exec":
			s.handleExecPage(w, r, id)
			return
		case "forwards":
			s.handleClientForwardsPage(w, r, id)
			return
		}
		http.NotFound(w, r)
		return
	}

	sum, ok := users.GetClientSummary(id)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, fmt.Errorf("client %q not found (maybe disconnected)", id))
		return
	}
	keyLabel := sum.Fingerprint
	if sum.Comment != "" {
		keyLabel = sum.Comment
	}
	owners := sum.Owners
	if owners == "" {
		owners = "public"
	}

	meta, _, _ := data.GetClientMeta(sum.Fingerprint)
	comm, _, _ := data.GetClientCommSettings(sum.Fingerprint)

	s.render(w, r, http.StatusOK, "client.html", map[string]any{
		"Base": pageBase{
			Title:   "Client " + id,
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Client": map[string]string{
			"ID":                   id,
			"Fingerprint":          sum.Fingerprint,
			"Status":               sum.Status,
			"LastSeen":             sum.LastSeen.Format(time.RFC3339),
			"Hostname":             sum.Hostname,
			"RemoteAddr":           sum.RemoteAddr,
			"Owners":               owners,
			"KeyLabel":             keyLabel,
			"Version":              sum.Version,
			"OS":                   parseClientOS(sum.Version),
			"Group":                meta.Group,
			"Note":                 meta.Note,
			"ServerTimeoutSeconds": fmt.Sprintf("%d", comm.ServerTimeoutSeconds),
			"ClientHeartbeatSec":   fmt.Sprintf("%d", comm.ClientHeartbeatSec),
			"SleepWindow":          comm.SleepWindow,
			"SleepCheckSec":        fmt.Sprintf("%d", comm.SleepCheckSec),
			"SleepUntil":           comm.SleepUntil,
		},
	})
}

func (s *Server) handleExecPage(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, fmt.Errorf("client %q not found (maybe disconnected)", id))
		return
	}
	keyLabel := sum.Fingerprint
	if sum.Comment != "" {
		keyLabel = sum.Comment
	}
	s.render(w, r, http.StatusOK, "exec.html", map[string]any{
		"Base": pageBase{
			Title:   "Exec " + id,
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Client": map[string]string{
			"ID":         id,
			"Status":     sum.Status,
			"Hostname":   sum.Hostname,
			"RemoteAddr": sum.RemoteAddr,
			"OS":         parseClientOS(sum.Version),
			"KeyLabel":   keyLabel,
		},
	})
}

func (s *Server) handleClientForwardsPage(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, fmt.Errorf("client %q not found (maybe disconnected)", id))
		return
	}
	clients, _ := users.ListConnectedClients("")
	s.render(w, r, http.StatusOK, "client_forwards.html", map[string]any{
		"Base": pageBase{
			Title:   "Forwards " + id,
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Client": map[string]string{
			"ID":          id,
			"Status":      sum.Status,
			"Fingerprint": sum.Fingerprint,
			"Hostname":    sum.Hostname,
			"RemoteAddr":  sum.RemoteAddr,
			"OS":          parseClientOS(sum.Version),
		},
		// Reuse connected list for datalists; this page primarily operates on the selected client.
		"Clients": clients,
	})
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	downloads, err := data.ListDownloads("")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err)
		return
	}

	type row struct {
		URLPath string
		OSArch  string
		Type    string
		Hits    int
		Size    string
		Version string
	}

	var rows []row
	for _, d := range downloads {
		rows = append(rows, row{
			URLPath: d.UrlPath,
			OSArch:  fmt.Sprintf("%s/%s%s", d.Goos, d.Goarch, d.Goarm),
			Type:    d.FileType,
			Hits:    d.Hits,
			Size:    fmt.Sprintf("%.1f KB", d.FileSize/1024),
			Version: d.Version,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].URLPath < rows[j].URLPath })

	s.render(w, r, http.StatusOK, "downloads.html", map[string]any{
		"Base": pageBase{
			Title:   "Downloads",
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Rows": rows,
	})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	tlsEnabled := false
	if multiplexer.ServerMultiplexer != nil && multiplexer.ServerMultiplexer.TLSEnabled() {
		tlsEnabled = true
	}
	s.render(w, r, http.StatusOK, "build.html", map[string]any{
		"Base": pageBase{
			Title:   "Build Client",
			Version: internal.Version,
			Now:     time.Now(),
		},
		"DefaultCallback":  webserver.DefaultConnectBack,
		"AutoCallback":     webserver.AutogeneratedConnectBack,
		"ServerTLSEnabled": tlsEnabled,
	})
}

func (s *Server) handleForwards(w http.ResponseWriter, r *http.Request) {
	clients, _ := users.ListConnectedClients("")
	s.render(w, r, http.StatusOK, "forwards.html", map[string]any{
		"Base": pageBase{
			Title:   "Forwards",
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Clients": clients,
	})
}

func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "webhooks.html", map[string]any{
		"Base": pageBase{
			Title:   "Webhooks",
			Version: internal.Version,
			Now:     time.Now(),
		},
	})
}

func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	clients, err := users.ListAllClients(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) handleAPIClientActions(w http.ResponseWriter, r *http.Request) {
	// /ui/api/clients/{id}/exec | /ui/api/clients/{id}/kill | /ui/api/clients/{id}/fs/...
	p := strings.TrimPrefix(r.URL.Path, "/ui/api/clients/")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]

	if action == "fs" {
		s.handleAPIClientFS(w, r, id, parts[2:])
		return
	}

	if action == "meta" {
		s.handleAPIClientMeta(w, r, id)
		return
	}
	if action == "comm" {
		s.handleAPIClientComm(w, r, id)
		return
	}
	if action == "sleep" {
		s.handleAPIClientSleep(w, r, id)
		return
	}
	if action == "delete" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := users.DeleteKnownClient(id); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	conn, ok := users.GetClientConnByFingerprint(id)
	if !ok {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	switch action {
	case "exec":
		var body struct {
			Cmd string `json:"cmd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		body.Cmd = strings.TrimSpace(body.Cmd)
		if body.Cmd == "" {
			http.Error(w, "cmd is empty", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		out, err := execOnConn(ctx, conn, body.Cmd)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "stdout": out})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stdout": out})
		return

	case "kill":
		_, _, _ = conn.SendRequest("kill", false, nil)
		_ = conn.Close()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "owners":
		var body struct {
			Owners string `json:"owners"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := users.SetClientOwnershipByFingerprint(id, strings.TrimSpace(body.Owners)); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func (s *Server) handleAPIClientComm(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		m, _, err := data.GetClientCommSettings(sum.Fingerprint)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fingerprint": sum.Fingerprint, "settings": m})
		return
	case http.MethodPost:
		var body struct {
			ServerTimeoutSeconds int    `json:"server_timeout_seconds"`
			ClientHeartbeatSec   int    `json:"client_heartbeat_sec"`
			SleepWindow          string `json:"sleep_window"`
			SleepCheckSec        int    `json:"sleep_check_sec"`
			SleepUntil           string `json:"sleep_until"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if body.ServerTimeoutSeconds < 0 || body.ServerTimeoutSeconds > 3600 {
			http.Error(w, "server_timeout_seconds out of range (0-3600)", http.StatusBadRequest)
			return
		}
		if body.ClientHeartbeatSec < 0 || body.ClientHeartbeatSec > 3600 {
			http.Error(w, "client_heartbeat_sec out of range (0-3600)", http.StatusBadRequest)
			return
		}
		if body.SleepCheckSec < 0 || body.SleepCheckSec > 3600 {
			http.Error(w, "sleep_check_sec out of range (0-3600)", http.StatusBadRequest)
			return
		}
		body.SleepWindow = strings.TrimSpace(body.SleepWindow)
		if len(body.SleepWindow) > 32 {
			http.Error(w, "sleep_window too long", http.StatusBadRequest)
			return
		}
		if err := validateSleepWindow(body.SleepWindow); err != nil {
			http.Error(w, "sleep_window invalid: "+err.Error(), http.StatusBadRequest)
			return
		}
		body.SleepUntil = strings.TrimSpace(body.SleepUntil)
		if len(body.SleepUntil) > 40 {
			http.Error(w, "sleep_until too long", http.StatusBadRequest)
			return
		}
		if body.SleepUntil != "" {
			if _, err := time.Parse(time.RFC3339, body.SleepUntil); err != nil {
				http.Error(w, "sleep_until invalid (expected RFC3339)", http.StatusBadRequest)
				return
			}
		}

		// Persist
		_, err := data.UpsertClientCommSettings(sum.Fingerprint, data.ClientCommSettings{
			ServerTimeoutSeconds: body.ServerTimeoutSeconds,
			ClientHeartbeatSec:   body.ClientHeartbeatSec,
			SleepWindow:          body.SleepWindow,
			SleepCheckSec:        body.SleepCheckSec,
			SleepUntil:           body.SleepUntil,
		})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}

		// Apply immediately when connected
		conn, ok := users.GetClientConnByFingerprint(id)
		if ok {
			conn.Permissions.Extensions["server-timeout"] = strconv.Itoa(body.ServerTimeoutSeconds)
			_, _, _ = conn.SendRequest("keepalive-rssh@golang.org", false, []byte(strconv.Itoa(body.ServerTimeoutSeconds)))
			_, _, _ = conn.SendRequest("client-heartbeat@rssh", false, []byte(strconv.Itoa(body.ClientHeartbeatSec)))
			payload := mustJSON(map[string]any{"window": body.SleepWindow, "check": body.SleepCheckSec})
			_, _, _ = conn.SendRequest("sleep-window@rssh", false, []byte(payload))
			payload2 := mustJSON(map[string]any{"until": body.SleepUntil})
			_, _, _ = conn.SendRequest("sleep-until@rssh", false, []byte(payload2))
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPIClientSleep(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Mode     string `json:"mode"` // now|until|clear
		Minutes  int    `json:"minutes"`
		UntilRFC string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	until := ""
	switch mode {
	case "clear":
		until = ""
	case "now":
		if body.Minutes <= 0 || body.Minutes > 365*24*60 {
			http.Error(w, "minutes out of range", http.StatusBadRequest)
			return
		}
		until = time.Now().Add(time.Duration(body.Minutes) * time.Minute).Format(time.RFC3339)
	case "until":
		body.UntilRFC = strings.TrimSpace(body.UntilRFC)
		if body.UntilRFC == "" {
			http.Error(w, "until is empty", http.StatusBadRequest)
			return
		}
		if _, err := time.Parse(time.RFC3339, body.UntilRFC); err != nil {
			http.Error(w, "until invalid (expected RFC3339)", http.StatusBadRequest)
			return
		}
		until = body.UntilRFC
	default:
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}

	comm, _, _ := data.GetClientCommSettings(sum.Fingerprint)
	comm.SleepUntil = until
	if _, err := data.UpsertClientCommSettings(sum.Fingerprint, comm); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if conn, ok := users.GetClientConnByFingerprint(id); ok {
		payload := mustJSON(map[string]any{"until": until})
		_, _, _ = conn.SendRequest("sleep-until@rssh", false, []byte(payload))
		if mode != "clear" {
			_ = conn.Close()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sleep_until": until})
}

func (s *Server) handleAPIClientMeta(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		m, _, err := data.GetClientMeta(sum.Fingerprint)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fingerprint": sum.Fingerprint, "meta": m})
		return
	case http.MethodPost:
		var body struct {
			Group string `json:"group"`
			Note  string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		m, err := data.UpsertClientMeta(sum.Fingerprint, body.Group, body.Note)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fingerprint": sum.Fingerprint, "meta": m})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPIClientMetaBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Fingerprints []string `json:"fingerprints"`
		Group        *string  `json:"group"`
		Note         *string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(body.Fingerprints) == 0 {
		http.Error(w, "fingerprints is empty", http.StatusBadRequest)
		return
	}
	if body.Group == nil && body.Note == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updated := 0
	var errs []string
	for _, fp := range body.Fingerprints {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}
		if _, err := data.PatchClientMeta(fp, body.Group, body.Note); err != nil {
			errs = append(errs, fp+": "+err.Error())
			continue
		}
		updated++
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "updated": updated, "error": strings.Join(errs, "; ")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": updated})
}

func (s *Server) handleAPIBuild(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name             string `json:"name"`
		Comment          string `json:"comment"`
		Owners           string `json:"owners"`
		GOOS             string `json:"goos"`
		GOARCH           string `json:"goarch"`
		GOARM            string `json:"goarm"`
		ConnectBack      string `json:"connect_back"`
		Transport        string `json:"transport"` // "", tls,wss,ws,stdio,http,https
		Proxy            string `json:"proxy"`
		Fingerprint      string `json:"fingerprint"`
		SNI              string `json:"sni"`
		LogLevel         string `json:"log_level"`
		WorkingDirectory string `json:"working_directory"`
		NTLMProxyCreds   string `json:"ntlm_proxy_creds"`
		VersionString    string `json:"version_string"`

		UseHostHeader   bool `json:"use_host_header"`
		SharedObject    bool `json:"shared_object"`
		Garble          bool `json:"garble"`
		UPX             bool `json:"upx"`
		Lzma            bool `json:"lzma"`
		NoLibC          bool `json:"no_lib_c"`
		RawDownload     bool `json:"raw_download"`
		UseKerberosAuth bool `json:"use_kerberos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	connectBack := strings.TrimSpace(body.ConnectBack)
	if connectBack == "" {
		connectBack = webserver.DefaultConnectBack
	}
	transport := strings.ToLower(strings.TrimSpace(body.Transport))
	switch transport {
	case "", "tls", "wss", "ws", "stdio", "http", "https":
	default:
		http.Error(w, "invalid transport", http.StatusBadRequest)
		return
	}
	// Prevent generating clients that can never connect: wss/https/tls require server-side TLS.
	if transport == "tls" || transport == "wss" || transport == "https" {
		if multiplexer.ServerMultiplexer == nil || !multiplexer.ServerMultiplexer.TLSEnabled() {
			http.Error(w, "selected transport requires server TLS; start server with --tls or choose ws/http/(default)", http.StatusBadRequest)
			return
		}
	}
	if transport != "" && !strings.Contains(connectBack, "://") {
		connectBack = transport + "://" + connectBack
	}
	// Avoid surprising defaults (e.g. wss://host -> :443). UI expects explicit ports.
	if err := requireExplicitPort(connectBack); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := webserver.BuildConfig{
		Name:              strings.TrimSpace(body.Name),
		Comment:           strings.TrimSpace(body.Comment),
		Owners:            strings.TrimSpace(body.Owners),
		GOOS:              strings.TrimSpace(body.GOOS),
		GOARCH:            strings.TrimSpace(body.GOARCH),
		GOARM:             strings.TrimSpace(body.GOARM),
		ConnectBackAdress: connectBack,
		Fingerprint:       strings.TrimSpace(body.Fingerprint),
		Proxy:             strings.TrimSpace(body.Proxy),
		SNI:               strings.TrimSpace(body.SNI),
		LogLevel:          strings.TrimSpace(body.LogLevel),
		UseHostHeader:     body.UseHostHeader,
		SharedLibrary:     body.SharedObject,
		UPX:               body.UPX,
		Lzma:              body.Lzma,
		Garble:            body.Garble,
		DisableLibC:       body.NoLibC,
		RawDownload:       body.RawDownload,
		UseKerberosAuth:   body.UseKerberosAuth,
		WorkingDirectory:  strings.TrimSpace(body.WorkingDirectory),
		NTLMProxyCreds:    strings.TrimSpace(body.NTLMProxyCreds),
		VersionString:     strings.TrimSpace(body.VersionString),
	}

	url, err := webserver.Build(cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": url})
}

func requireExplicitPort(connectBack string) error {
	connectBack = strings.TrimSpace(connectBack)
	if connectBack == "" || strings.HasPrefix(connectBack, "stdio://") {
		return nil
	}

	if strings.Contains(connectBack, "://") {
		u, err := url.Parse(connectBack)
		if err != nil {
			return fmt.Errorf("invalid callback address: %w", err)
		}
		switch strings.ToLower(u.Scheme) {
		case "tls", "wss", "ws", "http", "https":
			if u.Port() == "" {
				return fmt.Errorf("callback address must include an explicit port, e.g. %s://%s:3232", u.Scheme, u.Hostname())
			}
		}
		return nil
	}

	// Raw ssh (no scheme): require host:port (":3232" is OK).
	if _, _, err := net.SplitHostPort(connectBack); err != nil {
		return fmt.Errorf("callback address must include an explicit port, e.g. 172.16.3.1:3232")
	}
	return nil
}

func (s *Server) handleAPIDownloadActions(w http.ResponseWriter, r *http.Request) {
	// /ui/api/downloads/{urlPath}/delete
	p := strings.TrimPrefix(r.URL.Path, "/ui/api/downloads/")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	urlPath, action := parts[0], parts[1]
	if action != "delete" {
		http.NotFound(w, r)
		return
	}
	if err := data.DeleteDownload(urlPath); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAPIServerForwards(w http.ResponseWriter, r *http.Request) {
	if multiplexer.ServerMultiplexer == nil {
		http.Error(w, "server multiplexer not ready", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "listeners": multiplexer.ServerMultiplexer.GetListeners()})
		return
	case http.MethodPost:
		var body struct {
			Action string `json:"action"` // on/off
			Addr   string `json:"addr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		addr := strings.TrimSpace(body.Addr)
		if addr == "" {
			http.Error(w, "addr is empty", http.StatusBadRequest)
			return
		}
		switch strings.ToLower(body.Action) {
		case "on":
			if err := multiplexer.ServerMultiplexer.StartListener("tcp", addr); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		case "off":
			if err := multiplexer.ServerMultiplexer.StopListener(addr); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPIClientForwards(w http.ResponseWriter, r *http.Request) {
	// /ui/api/forwards/client/{clientOrPattern}
	p := strings.TrimPrefix(r.URL.Path, "/ui/api/forwards/client/")
	p = strings.Trim(p, "/")
	if p == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// only supports single id
		conn, ok := users.GetClientConn(p)
		if !ok {
			http.Error(w, "client not found", http.StatusNotFound)
			return
		}
		forwards, err := queryClientForwards(conn)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "forwards": forwards})
		return

	case http.MethodPost:
		var body struct {
			Action string `json:"action"` // on/off
			Addr   string `json:"addr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		addr := strings.TrimSpace(body.Addr)
		if addr == "" {
			http.Error(w, "addr is empty", http.StatusBadRequest)
			return
		}

		conns, err := users.SearchAllClientConns(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(conns) == 0 {
			http.Error(w, "no clients matched", http.StatusNotFound)
			return
		}

		req, err := parseRemoteForward(addr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payload := ssh.Marshal(&req)

		applied := 0
		var errs []string
		for id, conn := range conns {
			switch strings.ToLower(body.Action) {
			case "on":
				ok, msg, err := conn.SendRequest("tcpip-forward", true, payload)
				if err != nil || !ok {
					errs = append(errs, fmt.Sprintf("%s: %v %s", id, err, strings.TrimSpace(string(msg))))
					continue
				}
				applied++
			case "off":
				ok, msg, err := conn.SendRequest("cancel-tcpip-forward", true, payload)
				if err != nil || !ok {
					errs = append(errs, fmt.Sprintf("%s: %v %s", id, err, strings.TrimSpace(string(msg))))
					continue
				}
				applied++
			default:
				http.Error(w, "invalid action", http.StatusBadRequest)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": len(errs) == 0, "applied": applied, "errors": errs})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func parseRemoteForward(addr string) (internal.RemoteForwardRequest, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return internal.RemoteForwardRequest{}, err
	}
	p, err := strconv.ParseUint(port, 10, 32)
	if err != nil {
		return internal.RemoteForwardRequest{}, err
	}
	return internal.RemoteForwardRequest{
		BindAddr: host,
		BindPort: uint32(p),
	}, nil
}

func queryClientForwards(conn *ssh.ServerConn) ([]string, error) {
	ok, message, err := conn.SendRequest("query-tcpip-forwards", true, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("client does not support querying server forwards")
	}
	var f struct {
		RemoteForwards []string
	}
	if err := ssh.Unmarshal(message, &f); err != nil {
		return nil, err
	}
	return f.RemoteForwards, nil
}

func (s *Server) handleAPIWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hooks, err := data.GetAllWebhooks()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "webhooks": hooks})
		return
	case http.MethodPost:
		var body struct {
			Action   string `json:"action"` // add/delete
			URL      string `json:"url"`
			CheckTLS bool   `json:"check_tls"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		switch strings.ToLower(strings.TrimSpace(body.Action)) {
		case "add":
			u, err := data.CreateWebhook(strings.TrimSpace(body.URL), body.CheckTLS)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": u})
			return
		case "delete":
			if err := data.DeleteWebhook(strings.TrimSpace(body.URL)); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		default:
			http.Error(w, "invalid action", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPIClientFS(w http.ResponseWriter, r *http.Request, id string, rest []string) {
	if len(rest) < 1 {
		http.NotFound(w, r)
		return
	}
	action := rest[0]

	conn, ok := users.GetClientConnByFingerprint(id)
	if !ok {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	switch action {
	case "list":
		dir := r.URL.Query().Get("path")
		if dir == "" {
			dir = "/"
		}
		entries, err := sftpListDir(conn, dir)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": dir, "entries": entries})
		return
	case "read":
		p := r.URL.Query().Get("path")
		if p == "" {
			http.Error(w, "path is empty", http.StatusBadRequest)
			return
		}
		offset := int64(0)
		if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				offset = n
			}
		}
		maxBytes := int64(512 * 1024)
		if v := strings.TrimSpace(r.URL.Query().Get("max")); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				maxBytes = n
			}
		}

		content, encoding, truncated, meta, err := sftpReadFileChunk(conn, p, offset, maxBytes)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		readOnly := encoding != "utf-8" || truncated || offset != 0 || (meta.Size > 0 && maxBytes < meta.Size)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"path":      p,
			"content":   content,
			"truncated": truncated,
			"encoding":  encoding,
			"offset":    offset,
			"max":       maxBytes,
			"meta":      meta,
			"read_only": readOnly,
		})
		return
	case "write":
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Path) == "" {
			http.Error(w, "path is empty", http.StatusBadRequest)
			return
		}
		if err := sftpWriteTextFile(conn, strings.TrimSpace(body.Path), body.Content); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "download":
		p := r.URL.Query().Get("path")
		if p == "" {
			http.Error(w, "path is empty", http.StatusBadRequest)
			return
		}
		s.serveSFTPDownload(w, r, conn, p)
		return
	case "open":
		p := r.URL.Query().Get("path")
		if p == "" {
			http.Error(w, "path is empty", http.StatusBadRequest)
			return
		}
		s.serveSFTPSafeImageInline(w, r, conn, p)
		return
	case "upload":
		dir := r.URL.Query().Get("path")
		if dir == "" {
			dir = "/"
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		if err := sftpUploadFile(conn, dir, file, header); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "mkdir":
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := sftpMkdir(conn, strings.TrimSpace(body.Path)); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "rm":
		var body struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := sftpRemove(conn, strings.TrimSpace(body.Path), body.Recursive); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case "rename":
		var body struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := sftpRename(conn, strings.TrimSpace(body.From), strings.TrimSpace(body.To)); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	default:
		http.NotFound(w, r)
		return
	}
}

type sftpEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	UID     uint32    `json:"uid"`
	GID     uint32    `json:"gid"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

func withSFTP(conn *ssh.ServerConn, fn func(*sftp.Client) error) error {
	ch, reqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		return err
	}
	defer ch.Close()
	go ssh.DiscardRequests(reqs)

	var req struct{ Name string }
	req.Name = "sftp"
	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(&req))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("client refused sftp subsystem")
	}

	c, err := sftp.NewClientPipe(ch, ch)
	if err != nil {
		return err
	}
	defer c.Close()

	return fn(c)
}

func sftpListDir(conn *ssh.ServerConn, dir string) ([]sftpEntry, error) {
	var out []sftpEntry
	err := withSFTP(conn, func(c *sftp.Client) error {
		list, err := c.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, fi := range list {
			p := strings.TrimSuffix(dir, "/") + "/" + fi.Name()
			var uid, gid uint32
			if sys := fi.Sys(); sys != nil {
				if fs, ok := sys.(*sftp.FileStat); ok {
					uid = fs.UID
					gid = fs.GID
				} else if fs, ok := sys.(sftp.FileStat); ok {
					uid = fs.UID
					gid = fs.GID
				}
			}
			out = append(out, sftpEntry{
				Name:    fi.Name(),
				Path:    p,
				Size:    fi.Size(),
				Mode:    fi.Mode().String(),
				UID:     uid,
				GID:     gid,
				ModTime: fi.ModTime(),
				IsDir:   fi.IsDir(),
			})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].IsDir != out[j].IsDir {
				return out[i].IsDir
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
		return nil
	})
	return out, err
}

func (s *Server) serveSFTPDownload(w http.ResponseWriter, r *http.Request, conn *ssh.ServerConn, filePath string) {
	err := withSFTP(conn, func(c *sftp.Client) error {
		f, err := c.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		name := path.Base(filePath)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}

func (s *Server) serveSFTPSafeImageInline(w http.ResponseWriter, r *http.Request, conn *ssh.ServerConn, filePath string) {
	ext := strings.ToLower(path.Ext(filePath))
	contentType := ""
	isSVG := false
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".bmp":
		contentType = "image/bmp"
	case ".ico":
		contentType = "image/x-icon"
	case ".svg":
		contentType = "image/svg+xml"
		isSVG = true
	default:
		http.Error(w, "unsupported image type", http.StatusBadRequest)
		return
	}

	err := withSFTP(conn, func(c *sftp.Client) error {
		f, err := c.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		name := path.Base(filePath)
		// Defense-in-depth for inline previews (especially SVG).
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))

		if isSVG {
			// Best-effort sanitization to avoid script execution when embedded.
			// Note: this is not a full sanitizer, but it removes the most common active content patterns.
			const maxSVG = 512 * 1024
			b, err := io.ReadAll(io.LimitReader(f, maxSVG+1))
			if err != nil {
				return err
			}
			if len(b) > maxSVG {
				return fmt.Errorf("svg too large (max %d bytes)", maxSVG)
			}
			svg := string(b)

			// Remove <script>...</script>
			reScript := regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
			svg = reScript.ReplaceAllString(svg, "")
			// Remove on*="..." and on*='...'
			reOnAttr := regexp.MustCompile(`(?is)\son[a-z0-9_-]+\s*=\s*("[^"]*"|'[^']*')`)
			svg = reOnAttr.ReplaceAllString(svg, "")
			// Remove javascript: in href-like attributes
			reJS := regexp.MustCompile(`(?is)(xlink:href|href)\s*=\s*("\s*javascript:[^"]*"|'\s*javascript:[^']*')`)
			svg = reJS.ReplaceAllString(svg, "")

			_, err = io.Copy(w, strings.NewReader(svg))
			return err
		}

		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}

func sftpUploadFile(conn *ssh.ServerConn, dir string, file multipart.File, header *multipart.FileHeader) error {
	return withSFTP(conn, func(c *sftp.Client) error {
		dst := strings.TrimSuffix(dir, "/") + "/" + header.Filename
		f, err := c.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, file)
		return err
	})
}

func sftpMkdir(conn *ssh.ServerConn, p string) error {
	if p == "" {
		return errors.New("path is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error { return c.MkdirAll(p) })
}

func sftpRemove(conn *ssh.ServerConn, p string, recursive bool) error {
	if p == "" {
		return errors.New("path is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error {
		if !recursive {
			return c.Remove(p)
		}
		return c.RemoveAll(p)
	})
}

func sftpRename(conn *ssh.ServerConn, from, to string) error {
	if from == "" || to == "" {
		return errors.New("from/to is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error { return c.Rename(from, to) })
}

func sftpReadTextFile(conn *ssh.ServerConn, filePath string, maxBytes int64) (content string, truncated bool, err error) {
	var out []byte
	err = withSFTP(conn, func(c *sftp.Client) error {
		f, err := c.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err != nil {
			return err
		}
		if int64(len(b)) > maxBytes {
			truncated = true
			b = b[:maxBytes]
		}
		out = b
		return nil
	})
	if err != nil {
		return "", false, err
	}
	// best-effort: treat as UTF-8; editor is for text files
	return string(out), truncated, nil
}

type sftpFileMeta struct {
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

func sftpReadFileChunk(conn *ssh.ServerConn, filePath string, offset int64, maxBytes int64) (content string, encoding string, truncated bool, meta sftpFileMeta, err error) {
	if offset < 0 {
		offset = 0
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	// hard cap to keep UI snappy
	if maxBytes > 1024*1024 {
		maxBytes = 1024 * 1024
	}

	var out []byte
	err = withSFTP(conn, func(c *sftp.Client) error {
		f, err := c.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		if fi, err := f.Stat(); err == nil {
			meta.Size = fi.Size()
			meta.Mode = fi.Mode().String()
			meta.ModTime = fi.ModTime()
		}

		if meta.Size > 0 && offset >= meta.Size {
			out = []byte{}
			truncated = false
			return nil
		}

		// Read up to maxBytes+1 to detect truncation.
		buf := make([]byte, int(maxBytes)+1)
		n, _ := f.ReadAt(buf, offset)
		if n < 0 {
			n = 0
		}
		b := buf[:n]
		if int64(len(b)) > maxBytes {
			truncated = true
			b = b[:maxBytes]
		} else if meta.Size > 0 && offset+int64(len(b)) < meta.Size {
			// If we didn't reach EOF, more data exists.
			truncated = true
		}
		out = b
		return nil
	})
	if err != nil {
		return "", "", false, sftpFileMeta{}, err
	}

	if utf8.Valid(out) {
		return string(out), "utf-8", truncated, meta, nil
	}

	// Binary-ish: return hex dump for safer viewing.
	const bytesPerLine = 16
	var sb strings.Builder
	for i := 0; i < len(out); i += bytesPerLine {
		end := i + bytesPerLine
		if end > len(out) {
			end = len(out)
		}
		chunk := out[i:end]
		sb.WriteString(fmt.Sprintf("%08x  ", offset+int64(i)))
		for j := 0; j < bytesPerLine; j++ {
			if j < len(chunk) {
				sb.WriteString(fmt.Sprintf("%02x ", chunk[j]))
			} else {
				sb.WriteString("   ")
			}
			if j == 7 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString(" |")
		for _, c := range chunk {
			if c >= 32 && c < 127 {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String(), "hex", truncated, meta, nil
}

func sftpWriteTextFile(conn *ssh.ServerConn, filePath string, content string) error {
	return withSFTP(conn, func(c *sftp.Client) error {
		f, err := c.OpenFile(filePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, strings.NewReader(content))
		return err
	})
}

func (s *Server) handleFilesPage(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, fmt.Errorf("client %q not found (maybe disconnected)", id))
		return
	}
	if sum.Status != "connected" {
		s.renderError(w, r, http.StatusBadRequest, fmt.Errorf("client %q is offline", id))
		return
	}
	s.render(w, r, http.StatusOK, "files.html", map[string]any{
		"Base": pageBase{
			Title:   "Files " + id,
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Client": sum,
	})
}

func (s *Server) handleShellPage(w http.ResponseWriter, r *http.Request, id string) {
	sum, ok := users.GetClientSummary(id)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, fmt.Errorf("client %q not found (maybe disconnected)", id))
		return
	}
	if sum.Status != "connected" {
		s.renderError(w, r, http.StatusBadRequest, fmt.Errorf("client %q is offline", id))
		return
	}
	s.render(w, r, http.StatusOK, "shell.html", map[string]any{
		"Base": pageBase{
			Title:   "Shell " + id,
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Client": sum,
	})
}

func (s *Server) handleShellWS(ws *websocket.Conn) {
	req := ws.Request()
	p := strings.TrimPrefix(req.URL.Path, "/ui/ws/clients/")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 || parts[1] != "shell" {
		_ = websocket.Message.Send(ws, `{"type":"error","error":"not found"}`)
		return
	}
	id := parts[0]
	conn, ok := users.GetClientConnByFingerprint(id)
	if !ok {
		_ = websocket.Message.Send(ws, `{"type":"error","error":"client not found"}`)
		return
	}

	cols := uint32(120)
	rows := uint32(30)
	if v := req.URL.Query().Get("cols"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 10 && n < 400 {
			cols = uint32(n)
		}
	}
	if v := req.URL.Query().Get("rows"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 5 && n < 200 {
			rows = uint32(n)
		}
	}

	ch, chReqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		_ = websocket.Message.Send(ws, `{"type":"error","error":"open channel failed"}`)
		return
	}
	defer ch.Close()
	go ssh.DiscardRequests(chReqs)

	pty := internal.PtyReq{
		Term:    "xterm-256color",
		Columns: cols,
		Rows:    rows,
		Width:   0,
		Height:  0,
		Modes:   "",
	}
	_, err = ch.SendRequest("pty-req", true, ssh.Marshal(pty))
	if err != nil {
		_ = websocket.Message.Send(ws, `{"type":"error","error":"pty-req failed"}`)
		return
	}
	_, err = ch.SendRequest("shell", true, ssh.Marshal(internal.ShellStruct{Cmd: ""}))
	if err != nil {
		_ = websocket.Message.Send(ws, `{"type":"error","error":"shell failed"}`)
		return
	}

	type outMsg struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	// remote -> ws
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8192)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				b64 := base64.StdEncoding.EncodeToString(buf[:n])
				_ = websocket.Message.Send(ws, mustJSON(outMsg{Type: "output", Data: b64}))
			}
			if err != nil {
				_ = websocket.Message.Send(ws, `{"type":"exit"}`)
				return
			}
		}
	}()

	// ws -> remote
	for {
		select {
		case <-done:
			return
		default:
		}
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			return
		}
		var in struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols uint32 `json:"cols"`
			Rows uint32 `json:"rows"`
		}
		if err := json.Unmarshal([]byte(msg), &in); err != nil {
			continue
		}
		switch in.Type {
		case "input":
			b, err := base64.StdEncoding.DecodeString(in.Data)
			if err != nil {
				continue
			}
			_, _ = ch.Write(b)
		case "resize":
			// RFC 4254: window-change, payload: uint32 cols, rows, width, height
			if in.Cols == 0 || in.Rows == 0 {
				continue
			}
			payload := struct {
				Columns uint32
				Rows    uint32
				Width   uint32
				Height  uint32
			}{
				Columns: in.Cols,
				Rows:    in.Rows,
				Width:   0,
				Height:  0,
			}
			_, _ = ch.SendRequest("window-change", false, ssh.Marshal(&payload))
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func validateSleepWindow(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return fmt.Errorf("expected HH:MM-HH:MM")
	}
	if _, err := parseHHMM(parts[0]); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if _, err := parseHHMM(parts[1]); err != nil {
		return fmt.Errorf("end: %w", err)
	}
	return nil
}

func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute")
	}
	return h*60 + m, nil
}

func execOnConn(ctx context.Context, conn *ssh.ServerConn, cmd string) (string, error) {
	var payload struct {
		Cmd string
	}
	payload.Cmd = cmd

	commandByte := ssh.Marshal(&payload)

	ch, reqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		return "", err
	}
	defer ch.Close()
	go ssh.DiscardRequests(reqs)

	ok, err := ch.SendRequest("exec", true, commandByte)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("client refused exec request")
	}

	const maxOut = 1 << 20 // 1 MiB
	type result struct {
		out string
	}
	done := make(chan result, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(ch, maxOut))
		done <- result{out: string(b)}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		return res.out, nil
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.EnableSSE {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	events := make(chan observers.ClientState, 32)
	observerID := observers.ConnectionState.Register(func(c observers.ClientState) {
		select {
		case events <- c:
		default:
			// drop if slow client
		}
	})
	defer observers.ConnectionState.Deregister(observerID)

	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-events:
			// Enrich events so the browser doesn't need to perform follow-up requests.
			payload := map[string]any{
				"status":     e.Status,
				"id":         e.Fingerprint,
				"session_id": e.ID,
				"ip":         e.IP,
				"hostName":   e.HostName,
				"version":    e.Version,
				"timestamp":  e.Timestamp,
			}

			if fp := strings.TrimSpace(e.Fingerprint); fp != "" {
				if sum, ok := users.GetClientSummary(fp); ok {
					payload["hostname"] = sum.Hostname
					payload["remote_addr"] = sum.RemoteAddr
					payload["owners"] = func() string {
						if strings.TrimSpace(sum.Owners) == "" {
							return "public"
						}
						return sum.Owners
					}()
					payload["fingerprint"] = sum.Fingerprint
					payload["comment"] = sum.Comment
					payload["os"] = parseClientOS(sum.Version)
					payload["status"] = sum.Status
					payload["last_seen"] = sum.LastSeen

					keyLabel := sum.Fingerprint
					if sum.Comment != "" {
						keyLabel = sum.Comment
					}
					payload["key_label"] = keyLabel

					if m, _, err := data.GetClientMeta(sum.Fingerprint); err == nil {
						payload["group"] = m.Group
						payload["note"] = m.Note
					}
				}
			}

			b, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: client\ndata: %s\n\n", strings.ReplaceAll(string(b), "\n", ""))
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleAPITunnels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tunnels": tunnelManager.List(), "allowlist_count": tunnels.AllowlistCount()})
		return
	case http.MethodPost:
		var body struct {
			Fingerprint string `json:"fingerprint"`
			ListenPort  string `json:"listen_port"`
			Target      string `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		t, err := tunnelManager.Create(body.Fingerprint, body.ListenPort, body.Target)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tunnel": t})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPITunnelActions(w http.ResponseWriter, r *http.Request) {
	// /ui/api/tunnels/{id}/close
	p := strings.TrimPrefix(r.URL.Path, "/ui/api/tunnels/")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	if r.Method != http.MethodPost || action != "close" {
		http.NotFound(w, r)
		return
	}
	if err := tunnelManager.Close(id); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAPISOCKS(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxies": socksManager.List(), "allowlist_count": tunnels.AllowlistCount()})
		return
	case http.MethodPost:
		var body struct {
			Fingerprint string `json:"fingerprint"`
			BindAddr    string `json:"bind_addr"`
			ListenPort  string `json:"listen_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		p, err := socksManager.Create(body.Fingerprint, body.BindAddr, body.ListenPort)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": p})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleAPISOCKSActions(w http.ResponseWriter, r *http.Request) {
	// /ui/api/socks/{id}/close
	p := strings.TrimPrefix(r.URL.Path, "/ui/api/socks/")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	if r.Method != http.MethodPost || action != "close" {
		http.NotFound(w, r)
		return
	}
	if err := socksManager.Close(id); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"img-src 'self' data:",
		"style-src 'self' 'unsafe-inline'",
		"script-src 'self'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	}, "; "))

	// Keep URLs consistent if user clicks around.
	_ = r.URL

	tpl, ok := s.tpl[path.Base(name)]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	if err := tpl.ExecuteTemplate(w, path.Base(name), data); err != nil {
		log.Println("ui template error:", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, err error) {
	s.render(w, r, status, "error.html", map[string]any{
		"Base": pageBase{
			Title:   "Error",
			Version: internal.Version,
			Now:     time.Now(),
		},
		"Status": status,
		"Error":  err.Error(),
	})
}

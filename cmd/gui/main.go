package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"rssh/internal"
	"rssh/internal/server"
	"rssh/internal/server/data"
	"rssh/internal/server/multiplexer"
	"rssh/internal/server/socksproxy"
	"rssh/internal/server/tunnels"
	"rssh/internal/server/users"
	"rssh/internal/server/webserver"
)

type serverConfig struct {
	ListenAddr            string
	DataDir               string
	ExternalAddress       string
	TLS                   bool
	TLSCert               string
	TLSKey                string
	Insecure              bool
	OpenProxy             bool
	EnableClientDownloads bool
	TimeoutSeconds        int
}

type guiState struct {
	mu sync.Mutex

	rt      *server.Runtime
	running bool
}

func main() {
	a := app.NewWithID("rssh.gui")
	a.Settings().SetTheme(newANSITheme(theme.DefaultTheme()))
	w := a.NewWindow("RSSH GUI")
	w.Resize(fyne.NewSize(1100, 720))

	state := &guiState{}

	cfg := &serverConfig{
		ListenAddr:            ":3232",
		DataDir:               ".",
		EnableClientDownloads: true,
		TimeoutSeconds:        5,
	}

	status := widget.NewLabel("Stopped")
	globalSelected := widget.NewLabel("No client selected")
	globalSelected.TextStyle = fyne.TextStyle{Italic: true}

	indicator := canvas.NewCircle(color.NRGBA{R: 220, G: 38, B: 38, A: 255})
	indicator.StrokeWidth = 0
	indicator.Resize(fyne.NewSize(10, 10))

	listenEntry := widget.NewEntry()
	listenEntry.SetText(cfg.ListenAddr)
	dataDirEntry := widget.NewEntry()
	dataDirEntry.SetText(cfg.DataDir)
	externalEntry := widget.NewEntry()
	externalEntry.SetText(cfg.ExternalAddress)

	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText(strconv.Itoa(cfg.TimeoutSeconds))

	insecureCheck := widget.NewCheck("", func(b bool) { cfg.Insecure = b })
	openProxyCheck := widget.NewCheck("", func(b bool) { cfg.OpenProxy = b })
	downloadsCheck := widget.NewCheck("", func(b bool) { cfg.EnableClientDownloads = b })
	downloadsCheck.SetChecked(cfg.EnableClientDownloads)

	tlsCheck := widget.NewCheck("", func(b bool) { cfg.TLS = b })
	tlsCertEntry := widget.NewEntry()
	tlsKeyEntry := widget.NewEntry()

	startStop := widget.NewButton("Start", nil)
	startStop.Disable()

	validateAndEnable := func() {
		startStop.Enable()
		if strings.TrimSpace(listenEntry.Text) == "" {
			startStop.Disable()
			return
		}
		if strings.TrimSpace(dataDirEntry.Text) == "" {
			startStop.Disable()
			return
		}
		if _, err := strconv.Atoi(strings.TrimSpace(timeoutEntry.Text)); err != nil {
			startStop.Disable()
			return
		}
	}

	listenEntry.OnChanged = func(s string) { cfg.ListenAddr = strings.TrimSpace(s); validateAndEnable() }
	dataDirEntry.OnChanged = func(s string) { cfg.DataDir = strings.TrimSpace(s); validateAndEnable() }
	externalEntry.OnChanged = func(s string) { cfg.ExternalAddress = strings.TrimSpace(s); validateAndEnable() }
	timeoutEntry.OnChanged = func(s string) { validateAndEnable() }
	tlsCertEntry.OnChanged = func(s string) { cfg.TLSCert = strings.TrimSpace(s) }
	tlsKeyEntry.OnChanged = func(s string) { cfg.TLSKey = strings.TrimSpace(s) }

	validateAndEnable()

	startServer := func() error {
		dataDir := strings.TrimSpace(cfg.DataDir)
		absDataDir, err := filepath.Abs(dataDir)
		if err != nil {
			return err
		}
		if st, err := os.Stat(absDataDir); err != nil || !st.IsDir() {
			return fmt.Errorf("datadir %q is not a directory or cannot be accessed", absDataDir)
		}

		timeout, _ := strconv.Atoi(strings.TrimSpace(timeoutEntry.Text))
		if timeout < 0 {
			return errors.New("timeout must be >= 0")
		}
		cfg.TimeoutSeconds = timeout

		connectBack := strings.TrimSpace(cfg.ExternalAddress)
		auto := false
		if connectBack == "" {
			connectBack = strings.TrimSpace(cfg.ListenAddr)
			auto = true
		}

		rt, err := server.Start(
			strings.TrimSpace(cfg.ListenAddr),
			absDataDir,
			connectBack,
			auto,
			strings.TrimSpace(cfg.TLSCert),
			strings.TrimSpace(cfg.TLSKey),
			cfg.Insecure,
			cfg.EnableClientDownloads,
			cfg.TLS,
			cfg.OpenProxy,
			cfg.TimeoutSeconds,
		)
		if err != nil {
			return err
		}

		state.mu.Lock()
		state.rt = rt
		state.running = true
		state.mu.Unlock()

		return nil
	}

	stopServer := func() {
		state.mu.Lock()
		rt := state.rt
		state.rt = nil
		state.running = false
		state.mu.Unlock()

		if rt != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = rt.Stop(ctx)
		}
	}

	startStop.OnTapped = func() {
		state.mu.Lock()
		running := state.running
		state.mu.Unlock()

		if !running {
			err := startServer()
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			status.SetText("Running")
			indicator.FillColor = color.NRGBA{R: 34, G: 197, B: 94, A: 255}
			indicator.Refresh()
			startStop.SetText("Stop")
			return
		}

		stopServer()
		status.SetText("Stopped")
		indicator.FillColor = color.NRGBA{R: 220, G: 38, B: 38, A: 255}
		indicator.Refresh()
		startStop.SetText("Start")
	}

	openDataDir := widget.NewButtonWithIcon("Choose…", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if u == nil {
				return
			}
			dataDirEntry.SetText(u.Path())
		}, w)
	})

	serverForm := widget.NewForm(
		widget.NewFormItem("Status", container.NewHBox(indicator, status)),
		widget.NewFormItem("Listen", listenEntry),
		widget.NewFormItem("Data Dir", container.NewBorder(nil, nil, nil, openDataDir, dataDirEntry)),
		widget.NewFormItem("External Addr", externalEntry),
		widget.NewFormItem("Timeout (s)", timeoutEntry),
		widget.NewFormItem("Insecure", insecureCheck),
		widget.NewFormItem("Open Proxy", openProxyCheck),
		widget.NewFormItem("Downloads", downloadsCheck),
		widget.NewFormItem("TLS", tlsCheck),
		widget.NewFormItem("TLS Cert", tlsCertEntry),
		widget.NewFormItem("TLS Key", tlsKeyEntry),
	)

	serverActions := container.NewHBox(
		startStop,
		widget.NewButtonWithIcon("Fingerprint", theme.VisibilityIcon(), func() {
			dataDir := strings.TrimSpace(dataDirEntry.Text)
			abs, err := filepath.Abs(dataDir)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			private, err := server.CreateOrLoadServerKeys(filepath.Join(abs, "id_ed25519"))
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			dialog.ShowInformation("Server Fingerprint", internal.FingerprintSHA256Hex(private.PublicKey()), w)
		}),
		layout.NewSpacer(),
	)

	serverTab := container.NewPadded(container.NewVBox(
		widget.NewCard("Server", "Start/stop the backend and configure networking.", container.NewVBox(serverForm, serverActions)),
		widget.NewCard("Tip", "", widget.NewLabel("Start the server first, then manage clients from the other pages.")),
	))

	var selectedMu sync.RWMutex
	selectedID := ""
	getSelectedID := func() string {
		selectedMu.RLock()
		defer selectedMu.RUnlock()
		return selectedID
	}
	setSelectedID := func(id string) {
		selectedMu.Lock()
		selectedID = strings.TrimSpace(id)
		selectedMu.Unlock()
	}

	tunnelMgr := tunnels.NewManager()
	socksMgr := socksproxy.NewManager()

	clientActions := newClientActionsUI(a, w, globalSelected, setSelectedID, tunnelMgr, socksMgr)
	clientsTab := newClientsUI(a, w, clientActions)

	type page struct {
		Name string
		Obj  fyne.CanvasObject
	}
	pages := []page{
		{"Server", serverTab},
		{"Clients", clientsTab},
		{"Exec", clientActions.execTab},
		{"Shell", clientActions.shellTab},
		{"Files", clientActions.filesTab},
		{"Network", newNetworkLaunchPage(a, w, getSelectedID, tunnelMgr, socksMgr)},
		{"Downloads", clientActions.downloadsTab},
		{"Webhooks", clientActions.webhooksTab},
	}

	content := container.NewMax(pages[0].Obj)
	nav := widget.NewList(
		func() int { return len(pages) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(pages[i].Name) },
	)
	nav.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(pages) {
			return
		}
		content.Objects = []fyne.CanvasObject{pages[id].Obj}
		content.Refresh()
	}
	nav.Select(0)

	navCard := widget.NewCard("Navigation", "", container.NewScroll(nav))
	topbar := container.NewHBox(
		widget.NewIcon(theme.ComputerIcon()),
		widget.NewLabelWithStyle("RSSH", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
	)
	statusbar := container.NewHBox(
		widget.NewLabelWithStyle("Selected:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		globalSelected,
		layout.NewSpacer(),
	)

	root := container.NewBorder(
		container.NewPadded(topbar),
		container.NewPadded(statusbar),
		container.NewPadded(navCard),
		nil,
		content,
	)
	w.SetContent(root)

	w.SetCloseIntercept(func() {
		stopServer()
		w.Close()
	})

	w.ShowAndRun()
}

type clientActionsUI struct {
	selectedID   *widget.Label
	selectedMeta *widget.Label

	execTab      fyne.CanvasObject
	shellTab     fyne.CanvasObject
	filesTab     fyne.CanvasObject
	forwardsTab  fyne.CanvasObject
	tunnelsTab   fyne.CanvasObject
	socksTab     fyne.CanvasObject
	webhooksTab  fyne.CanvasObject
	downloadsTab fyne.CanvasObject

	setSelected func(users.ClientSummary)
}

func newClientActionsUI(
	a fyne.App,
	w fyne.Window,
	globalSelected *widget.Label,
	onSelect func(string),
	tunnelMgr *tunnels.Manager,
	socksMgr *socksproxy.Manager,
) *clientActionsUI {
	selectedID := widget.NewLabel("None")
	selectedMeta := widget.NewLabel("")

	header := container.NewHBox(
		widget.NewLabelWithStyle("Selected:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		selectedID,
		layout.NewSpacer(),
		selectedMeta,
	)

	page := func(title, subtitle string, body fyne.CanvasObject) fyne.CanvasObject {
		card := widget.NewCard(title, subtitle, body)
		return container.NewBorder(container.NewPadded(header), nil, nil, nil, container.NewPadded(card))
	}

	getConn := func(id string) (*ssh.ServerConn, error) {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("no client selected")
		}
		conn, ok := users.GetClientConnByFingerprint(id)
		if !ok {
			return nil, errors.New("client not connected")
		}
		return conn, nil
	}

	execOut := newANSITextView()

	execCmd := widget.NewEntry()
	execCmd.SetPlaceHolder("Command to exec on client (non-interactive)")

	execBtn := widget.NewButton("Run", func() {})
	execCopy := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(execOut.Text())
	})
	execClear := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		execOut.Clear()
	})
	execBtn.Disable()

	execTop := container.NewVBox(
		widget.NewLabelWithStyle("Command", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, container.NewHBox(execBtn, execCopy, execClear), execCmd),
	)
	execBody := container.NewBorder(execTop, nil, nil, nil, execOut.Object())
	execTab := page("Exec", "Run a one-shot command on the selected client.", execBody)

	var shellSessMu sync.Mutex
	var shellSess *shellSession

	term := newTerminalWidget(120)
	clearTerm := widget.NewButton("Clear", func() { term.Clear() })
	copyTerm := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(term.PlainText())
	})
	shellConnect := widget.NewButton("Connect", func() {})
	shellDisconnect := widget.NewButton("Disconnect", func() {})
	shellConnect.Disable()
	shellDisconnect.Disable()

	appendShell := func(b []byte) { term.AppendOutput(b) }

	shellTop := container.NewHBox(shellConnect, shellDisconnect, copyTerm, clearTerm, layout.NewSpacer())
	shellBody := container.NewBorder(shellTop, nil, nil, nil, term.Object(w))
	shellTab := page("Shell", "Interactive shell (terminal emulator). Click inside to type.", shellBody)

	filesInfo := widget.NewLabel("Select a connected client to browse via SFTP.")
	filesPath := widget.NewEntry()
	filesPath.SetText("/")
	filesRefresh := widget.NewButton("List", func() {})
	filesUp := widget.NewButtonWithIcon("Up", theme.NavigateBackIcon(), func() {
		cur := strings.TrimSpace(filesPath.Text)
		if cur == "" || cur == "/" {
			filesPath.SetText("/")
			return
		}
		filesPath.SetText(path.Dir(strings.TrimRight(cur, "/")))
		filesRefresh.OnTapped()
	})
	filesClearCache := widget.NewButtonWithIcon("Clear Cache", theme.DeleteIcon(), func() {
		id := strings.TrimSpace(selectedID.Text)
		if id == "" || id == "None" {
			return
		}
		if err := data.DeleteFileCacheForFingerprint(id); err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("Files Cache", "Cleared", w)
	})
	filesRefresh.Disable()
	filesClearCache.Disable()

	var filesEntries []sftpEntry

	filesList := widget.NewList(
		func() int { return len(filesEntries) },
		func() fyne.CanvasObject {
			it := newFileListItem()
			it.onSecondary = func(index int, absPos fyne.Position) {
				if index < 0 || index >= len(filesEntries) {
					return
				}
				showFileContextMenu(w, func() (*ssh.ServerConn, string, sftpEntry, bool, error) {
					id := strings.TrimSpace(selectedID.Text)
					conn, err := getConn(id)
					if err != nil {
						return nil, "", sftpEntry{}, false, err
					}
					curDir := strings.TrimSpace(filesPath.Text)
					if curDir == "" {
						curDir = "/"
					}
					return conn, curDir, filesEntries[index], true, nil
				}, func() {
					filesRefresh.OnTapped()
				}, absPos)
			}
			it.onDouble = func(index int) {
				if index < 0 || index >= len(filesEntries) {
					return
				}
				e := filesEntries[index]
				if !e.IsDir {
					return
				}
				filesPath.SetText(e.Path)
				filesRefresh.OnTapped()
			}
			return it
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(filesEntries) {
				return
			}
			e := filesEntries[i]
			name := e.Name
			if e.IsDir {
				name += "/"
			}
			o.(*fileListItem).set(i, fmt.Sprintf("%s  %s  %d", e.Mode, name, e.Size))
		},
	)

	filesTop := container.NewVBox(filesInfo, container.NewBorder(nil, nil, widget.NewLabel("Path"), container.NewHBox(filesUp, filesRefresh, filesClearCache), filesPath))
	filesBody := container.NewBorder(filesTop, nil, nil, nil, container.NewScroll(filesList))
	filesTab := page("Files", "Browse files over SFTP (list/read).", filesBody)

	forwardsInfo := widget.NewLabel("Manage server listeners and client remote forwards.")
	serverListenAddr := widget.NewEntry()
	serverListenAddr.SetPlaceHolder("e.g. 0.0.0.0:2222")
	serverListenOn := widget.NewButton("Start Listener", func() {})
	serverListenOff := widget.NewButton("Stop Listener", func() {})
	serverListenOn.Disable()
	serverListenOff.Disable()

	clientForwardAddr := widget.NewEntry()
	clientForwardAddr.SetPlaceHolder("e.g. 127.0.0.1:3389")
	clientForwardOn := widget.NewButton("tcpip-forward on", func() {})
	clientForwardOff := widget.NewButton("cancel-tcpip-forward", func() {})
	clientForwardOn.Disable()
	clientForwardOff.Disable()

	forwardsBody := container.NewVBox(
		forwardsInfo,
		widget.NewSeparator(),
		widget.NewCard("Server Listeners", "Start/stop listeners on the RSSH server.",
			container.NewBorder(nil, nil, nil, container.NewHBox(serverListenOn, serverListenOff), serverListenAddr),
		),
		widget.NewCard("Client Remote Forward", "Request tcpip-forward on the selected client.",
			container.NewBorder(nil, nil, nil, container.NewHBox(clientForwardOn, clientForwardOff), clientForwardAddr),
		),
	)
	forwardsTab := page("Forwards", "Listeners and remote forwards.", forwardsBody)

	tunnelInfo := widget.NewLabel("Tunnel allowlist is loaded from datadir (tunnel_allow_cidrs.txt or RSSH_TUNNEL_ALLOW_CIDRS).")
	tunnelAllow := widget.NewMultiLineEntry()
	tunnelAllow.Disable()
	tunnelAllowRefresh := widget.NewButton("Refresh Allowlist", func() {
		entries := tunnels.AllowlistCIDRs()
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(e.String())
			sb.WriteString("\n")
		}
		tunnelAllow.SetText(sb.String())
	})

	tunnelListenPort := widget.NewEntry()
	tunnelListenPort.SetPlaceHolder("local listen port (e.g. 1081)")
	tunnelTarget := widget.NewEntry()
	tunnelTarget.SetPlaceHolder("target host:port (e.g. 10.0.0.5:22)")
	tunnelCreate := widget.NewButton("Create Tunnel", func() {})
	tunnelClose := widget.NewButton("Close Tunnel", func() {})
	tunnelCreate.Disable()
	tunnelClose.Disable()

	var activeTunnels []tunnels.Tunnel
	selectedTunnel := -1
	tunnelList := widget.NewList(
		func() int { return len(activeTunnels) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(activeTunnels) {
				return
			}
			t := activeTunnels[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s  %s -> %s", t.ID, t.ListenAddr, t.Target))
		},
	)
	tunnelList.OnSelected = func(id widget.ListItemID) {
		selectedTunnel = id
		if id >= 0 && id < len(activeTunnels) {
			tunnelClose.Enable()
		}
	}

	refreshTunnels := func() {
		activeTunnels = tunnelMgr.List()
		selectedTunnel = -1
		tunnelClose.Disable()
		tunnelList.Refresh()
	}

	tunnelsBody := container.NewVScroll(container.NewVBox(
		widget.NewCard("Allowlist", "Loaded from datadir and/or environment.", container.NewVBox(
			tunnelInfo,
			container.NewHBox(tunnelAllowRefresh, layout.NewSpacer()),
			container.NewScroll(tunnelAllow),
		)),
		widget.NewCard("Active Tunnels", "Local TCP listener that dials to target via selected client.", container.NewVBox(
			container.NewBorder(nil, nil, nil, tunnelCreate, container.NewGridWithColumns(2, tunnelListenPort, tunnelTarget)),
			container.NewHBox(tunnelClose, layout.NewSpacer()),
			container.NewScroll(tunnelList),
		)),
	))
	tunnelsTab := page("Tunnels", "Create local tunnels via a selected client.", tunnelsBody)

	socksBind := widget.NewEntry()
	socksBind.SetText("127.0.0.1")
	socksPort := widget.NewEntry()
	socksPort.SetPlaceHolder("port (e.g. 1080)")
	socksCreate := widget.NewButton("Create SOCKS", func() {})
	socksClose := widget.NewButton("Close SOCKS", func() {})
	socksCreate.Disable()
	socksClose.Disable()

	var activeSOCKS []socksproxy.Proxy
	selectedSOCKS := -1
	socksList := widget.NewList(
		func() int { return len(activeSOCKS) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(activeSOCKS) {
				return
			}
			p := activeSOCKS[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s  %s  (%s)", p.ID, p.ListenAddr, p.Fingerprint))
		},
	)
	socksList.OnSelected = func(id widget.ListItemID) {
		selectedSOCKS = id
		if id >= 0 && id < len(activeSOCKS) {
			socksClose.Enable()
		}
	}

	refreshSOCKS := func() {
		activeSOCKS = socksMgr.List()
		selectedSOCKS = -1
		socksClose.Disable()
		socksList.Refresh()
	}

	socksBody := container.NewVScroll(container.NewVBox(
		widget.NewCard("Create", "SOCKS5 listener that dials out through the selected client (CONNECT only).", container.NewVBox(
			container.NewBorder(nil, nil, nil, socksCreate, container.NewGridWithColumns(2, socksBind, socksPort)),
		)),
		widget.NewCard("Active SOCKS", "", container.NewVBox(
			container.NewHBox(socksClose, layout.NewSpacer()),
			container.NewScroll(socksList),
		)),
	))
	socksTab := page("SOCKS", "Create and manage local SOCKS5 proxies.", socksBody)

	webhookURL := widget.NewEntry()
	webhookURL.SetPlaceHolder("https://example.com/webhook")
	webhookCheckTLS := widget.NewCheck("Check TLS", func(bool) {})
	webhookCheckTLS.SetChecked(true)
	webhookAdd := widget.NewButton("Add", func() {})
	webhookDel := widget.NewButton("Delete", func() {})
	webhookAdd.Disable()
	webhookDel.Disable()

	var webhooks []data.Webhook
	selectedWebhook := -1
	webhookList := widget.NewList(
		func() int { return len(webhooks) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(webhooks) {
				return
			}
			wh := webhooks[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s  tls=%t", wh.URL, wh.CheckTLS))
		},
	)
	webhookList.OnSelected = func(id widget.ListItemID) {
		selectedWebhook = id
		if id >= 0 && id < len(webhooks) {
			webhookDel.Enable()
		}
	}

	refreshWebhooks := func() {
		out, err := data.GetAllWebhooks()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		webhooks = out
		selectedWebhook = -1
		webhookDel.Disable()
		webhookList.Refresh()
	}

	webhooksBody := container.NewVScroll(container.NewVBox(
		widget.NewCard("Create", "Webhooks fire on client connect/disconnect events.", container.NewVBox(
			container.NewBorder(nil, nil, nil, container.NewHBox(webhookCheckTLS, webhookAdd), webhookURL),
		)),
		widget.NewCard("Existing", "", container.NewVBox(
			container.NewHBox(webhookDel, widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshWebhooks), layout.NewSpacer()),
			container.NewScroll(webhookList),
		)),
	))
	webhooksTab := page("Webhooks", "Notify external systems on events.", webhooksBody)

	dlFilter := widget.NewEntry()
	dlFilter.SetPlaceHolder("filter (glob, optional)")
	dlRefresh := widget.NewButton("Refresh", func() {})
	dlDelete := widget.NewButton("Delete", func() {})
	dlRefresh.Disable()
	dlDelete.Disable()

	var downloads []data.Download
	selectedDownload := -1
	dlList := widget.NewList(
		func() int { return len(downloads) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(downloads) {
				return
			}
			d := downloads[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s  %s/%s%s  hits=%d  %.2fMB", d.UrlPath, d.Goos, d.Goarch, d.Goarm, d.Hits, d.FileSize))
		},
	)
	dlList.OnSelected = func(id widget.ListItemID) {
		selectedDownload = id
		if id >= 0 && id < len(downloads) {
			dlDelete.Enable()
		}
	}

	refreshDownloads := func() {
		filter := strings.TrimSpace(dlFilter.Text)
		if filter == "" {
			filter = "*"
		}
		m, err := data.ListDownloads(filter)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		downloads = downloads[:0]
		for _, v := range m {
			downloads = append(downloads, v)
		}
		selectedDownload = -1
		dlDelete.Disable()
		dlList.Refresh()
	}

	buildName := widget.NewEntry()
	buildGOOS := widget.NewEntry()
	buildGOARCH := widget.NewEntry()
	buildCallback := widget.NewEntry()
	buildFingerprint := widget.NewEntry()
	buildLogLevel := widget.NewEntry()
	buildLogLevel.SetText("INFO")
	buildBtn := widget.NewButton("Build Client", func() {})
	buildBtn.Disable()
	buildOut := widget.NewLabel("")

	downloadsBody := container.NewVScroll(container.NewVBox(
		widget.NewCard("Build", "Build a client binary into the downloads cache (requires downloads enabled).", container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Name", buildName),
				widget.NewFormItem("GOOS", buildGOOS),
				widget.NewFormItem("GOARCH", buildGOARCH),
				widget.NewFormItem("Callback Addr", buildCallback),
				widget.NewFormItem("Fingerprint", buildFingerprint),
				widget.NewFormItem("Log Level", buildLogLevel),
			),
			container.NewHBox(buildBtn, layout.NewSpacer(), buildOut),
		)),
		widget.NewCard("Downloads", "Manage built artifacts.", container.NewVBox(
			container.NewBorder(nil, nil, nil, container.NewHBox(dlRefresh, dlDelete), dlFilter),
			container.NewScroll(dlList),
		)),
	))
	downloadsTab := page("Downloads", "Build and manage client artifacts.", downloadsBody)

	ui := &clientActionsUI{
		selectedID:   selectedID,
		selectedMeta: selectedMeta,
		execTab:      execTab,
		shellTab:     shellTab,
		filesTab:     filesTab,
		forwardsTab:  forwardsTab,
		tunnelsTab:   tunnelsTab,
		socksTab:     socksTab,
		downloadsTab: downloadsTab,
		webhooksTab:  webhooksTab,
	}

	ui.setSelected = func(sum users.ClientSummary) {
		selectedID.SetText(sum.ID)
		selectedMeta.SetText(sum.Hostname + " (" + sum.Status + ")")
		if globalSelected != nil {
			globalSelected.SetText(sum.ID + "  " + sum.Hostname + "  [" + sum.Status + "]")
		}
		if onSelect != nil {
			onSelect(sum.ID)
		}

		execBtn.Enable()
		shellConnect.Enable()
		filesRefresh.Enable()
		filesClearCache.Enable()
		serverListenOn.Enable()
		serverListenOff.Enable()
		clientForwardOn.Enable()
		clientForwardOff.Enable()
		tunnelCreate.Enable()
		socksCreate.Enable()

		if sum.Status != "connected" {
			execBtn.Disable()
			shellConnect.Disable()
			filesRefresh.Disable()
			filesClearCache.Disable()
			clientForwardOn.Disable()
			clientForwardOff.Disable()
			tunnelCreate.Disable()
			socksCreate.Disable()
		}
	}

	execBtn.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		cmd := strings.TrimSpace(execCmd.Text)
		if cmd == "" {
			return
		}
		conn, err := getConn(id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		execBtn.Disable()
		runAsync(w, "Exec", "Running command…", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			out, err := execOnConn(ctx, conn, cmd)
			fyne.Do(func() {
				if err != nil {
					execOut.SetText(out + "\n\nERROR: " + err.Error())
				} else {
					execOut.SetText(out)
				}
			})
			return nil
		}, func() {
			execBtn.Enable()
		})
	}
	execCmd.OnSubmitted = func(string) {
		if execBtn.Disabled() {
			return
		}
		execBtn.OnTapped()
	}

	connectShell := func(id string) {
		conn, err := getConn(id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		shellConnect.Disable()
		runAsync(w, "Shell", "Connecting…", func() error {
			sess, err := openShell(conn, 120, 30)
			if err != nil {
				return err
			}

			shellSessMu.Lock()
			if shellSess != nil {
				_ = shellSess.Close()
			}
			shellSess = sess
			shellSessMu.Unlock()

			fyne.Do(func() {
				term.Clear()
				term.SetSender(w, func(b []byte) { _, _ = sess.Write(b) })
				shellDisconnect.Enable()
			})

			go func() {
				buf := make([]byte, 8192)
				for {
					n, err := sess.Read(buf)
					if n > 0 {
						appendShell(buf[:n])
					}
					if err != nil {
						appendShell([]byte("\n[disconnected]\n"))
						fyne.Do(func() {
							term.SetSender(w, nil)
							shellDisconnect.Disable()
							shellConnect.Enable()
						})
						return
					}
				}
			}()

			return nil
		}, func() {
			// Once connected or on error, allow reconnect attempts.
			shellConnect.Enable()
		})
	}

	shellConnect.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		connectShell(id)
	}

	shellDisconnect.OnTapped = func() {
		shellSessMu.Lock()
		sess := shellSess
		shellSess = nil
		shellSessMu.Unlock()
		if sess != nil {
			_ = sess.Close()
		}
		term.SetSender(w, nil)
		shellDisconnect.Disable()
		shellConnect.Enable()
	}

	filesPath.OnSubmitted = func(string) { filesRefresh.OnTapped() }
	filesRefresh.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		dir := strings.TrimSpace(filesPath.Text)
		if dir == "" {
			dir = "/"
		}
		filesRefresh.Disable()
		runAsync(w, "Files", "Listing directory…", func() error {
			conn, err := getConn(id)
			if err != nil {
				return err
			}
			entries, err := sftpListDir(conn, dir)
			if err != nil {
				return err
			}
			sort.SliceStable(entries, func(i, j int) bool {
				if entries[i].IsDir != entries[j].IsDir {
					return entries[i].IsDir
				}
				return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
			})
			fyne.Do(func() {
				filesEntries = entries
				filesList.Refresh()
			})
			return nil
		}, func() {
			filesRefresh.Enable()
		})
	}

	filesList.OnSelected = func(i widget.ListItemID) {
		if i < 0 || i >= len(filesEntries) {
			return
		}
		id := strings.TrimSpace(selectedID.Text)
		conn, err := getConn(id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		e := filesEntries[i]
		if e.IsDir {
			return
		}
		content, enc, truncated, meta, err := sftpReadFileChunkCached(id, conn, e, 0, 128*1024)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		title := e.Path + " (" + enc + ")"
		if truncated {
			title += " [truncated]"
		}
		showTextPreviewDialog(w, title, fmt.Sprintf("%s\n\n%s", meta.Mode, content))
	}

	serverListenOn.OnTapped = func() {
		if multiplexer.ServerMultiplexer == nil {
			dialog.ShowError(errors.New("server not running"), w)
			return
		}
		addr := strings.TrimSpace(serverListenAddr.Text)
		if addr == "" {
			return
		}
		if err := multiplexer.ServerMultiplexer.StartListener("tcp", addr); err != nil {
			dialog.ShowError(err, w)
		}
	}
	serverListenOff.OnTapped = func() {
		if multiplexer.ServerMultiplexer == nil {
			dialog.ShowError(errors.New("server not running"), w)
			return
		}
		addr := strings.TrimSpace(serverListenAddr.Text)
		if addr == "" {
			return
		}
		if err := multiplexer.ServerMultiplexer.StopListener(addr); err != nil {
			dialog.ShowError(err, w)
		}
	}

	clientForwardOn.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		conn, err := getConn(id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		addr := strings.TrimSpace(clientForwardAddr.Text)
		if addr == "" {
			return
		}
		req, err := parseRemoteForward(addr)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		ok, msg, err := conn.SendRequest("tcpip-forward", true, ssh.Marshal(&req))
		if err != nil || !ok {
			dialog.ShowError(fmt.Errorf("request failed: %v %s", err, strings.TrimSpace(string(msg))), w)
			return
		}
	}
	clientForwardOff.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		conn, err := getConn(id)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		addr := strings.TrimSpace(clientForwardAddr.Text)
		if addr == "" {
			return
		}
		req, err := parseRemoteForward(addr)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		ok, msg, err := conn.SendRequest("cancel-tcpip-forward", true, ssh.Marshal(&req))
		if err != nil || !ok {
			dialog.ShowError(fmt.Errorf("request failed: %v %s", err, strings.TrimSpace(string(msg))), w)
			return
		}
	}

	tunnelCreate.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		port := strings.TrimSpace(tunnelListenPort.Text)
		target := strings.TrimSpace(tunnelTarget.Text)
		if port == "" || target == "" {
			return
		}
		_, err := tunnelMgr.Create(id, port, target)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		refreshTunnels()
	}
	tunnelClose.OnTapped = func() {
		if selectedTunnel < 0 || selectedTunnel >= len(activeTunnels) {
			return
		}
		id := activeTunnels[selectedTunnel].ID
		if err := tunnelMgr.Close(id); err != nil {
			dialog.ShowError(err, w)
			return
		}
		refreshTunnels()
	}

	socksCreate.OnTapped = func() {
		id := strings.TrimSpace(selectedID.Text)
		p, err := socksMgr.Create(id, strings.TrimSpace(socksBind.Text), strings.TrimSpace(socksPort.Text))
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("SOCKS Created", p.ListenAddr, w)
		refreshSOCKS()
	}
	socksClose.OnTapped = func() {
		if selectedSOCKS < 0 || selectedSOCKS >= len(activeSOCKS) {
			return
		}
		id := activeSOCKS[selectedSOCKS].ID
		if err := socksMgr.Close(id); err != nil {
			dialog.ShowError(err, w)
			return
		}
		refreshSOCKS()
	}

	webhookAdd.Enable()
	webhookDel.Disable()
	webhookAdd.OnTapped = func() {
		u := strings.TrimSpace(webhookURL.Text)
		if u == "" {
			return
		}
		_, err := data.CreateWebhook(u, webhookCheckTLS.Checked)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		webhookURL.SetText("")
		refreshWebhooks()
	}
	webhookDel.OnTapped = func() {
		if selectedWebhook < 0 || selectedWebhook >= len(webhooks) {
			return
		}
		if err := data.DeleteWebhook(webhooks[selectedWebhook].URL); err != nil {
			dialog.ShowError(err, w)
			return
		}
		refreshWebhooks()
	}

	dlRefresh.Enable()
	dlRefresh.OnTapped = refreshDownloads
	dlDelete.OnTapped = func() {
		if selectedDownload < 0 || selectedDownload >= len(downloads) {
			return
		}
		if err := data.DeleteDownload(downloads[selectedDownload].UrlPath); err != nil {
			dialog.ShowError(err, w)
			return
		}
		refreshDownloads()
	}

	buildBtn.Enable()
	buildBtn.OnTapped = func() {
		cfg := webserver.BuildConfig{
			Name:              strings.TrimSpace(buildName.Text),
			GOOS:              strings.TrimSpace(buildGOOS.Text),
			GOARCH:            strings.TrimSpace(buildGOARCH.Text),
			ConnectBackAdress: strings.TrimSpace(buildCallback.Text),
			Fingerprint:       strings.TrimSpace(buildFingerprint.Text),
			LogLevel:          strings.TrimSpace(buildLogLevel.Text),
		}
		p, err := webserver.Build(cfg)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		buildOut.SetText("Built: " + p)
		refreshDownloads()
	}

	tunnelAllowRefresh.OnTapped()
	refreshTunnels()
	refreshSOCKS()

	return ui
}

func newClientsUI(a fyne.App, w fyne.Window, actions *clientActionsUI) fyne.CanvasObject {
	filter := widget.NewEntry()
	filter.SetPlaceHolder("Search (substring)")

	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), nil)

	var rows []users.ClientSummary
	metas := map[string]data.ClientMeta{}
	var refresh func()

	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject {
			it := newClientListItem()
			it.onSecondary = func(index int, absPos fyne.Position) {
				if index < 0 || index >= len(rows) {
					return
				}
				sum := rows[index]
				actions.setSelected(sum)
				showClientContextMenu(w, sum, metas[sum.Fingerprint], refresh, absPos)
			}
			return it
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(rows) {
				return
			}
			s := rows[i]
			m := metas[s.Fingerprint]
			group := strings.TrimSpace(m.Group)
			if group == "" {
				group = "-"
			}
			note := strings.TrimSpace(m.Note)
			if note != "" {
				note = "  " + note
			}
			o.(*clientListItem).set(i, fmt.Sprintf("[%s]  %s  %s  %s  %s%s", group, s.Status, s.ID, s.Hostname, s.RemoteAddr, note))
		},
	)

	refresh = func() {
		q := strings.TrimSpace(filter.Text)
		go func() {
			out, err := users.ListAllClients(q)
			nextMetas := map[string]data.ClientMeta{}
			if err == nil {
				fps := make([]string, 0, len(out))
				for _, s := range out {
					fps = append(fps, s.Fingerprint)
				}
				if m, metaErr := data.ListClientMetas(fps); metaErr == nil {
					nextMetas = m
				}
				sort.SliceStable(out, func(i, j int) bool {
					gi := strings.ToLower(strings.TrimSpace(nextMetas[out[i].Fingerprint].Group))
					gj := strings.ToLower(strings.TrimSpace(nextMetas[out[j].Fingerprint].Group))
					if gi != gj {
						if gi == "" {
							return false
						}
						if gj == "" {
							return true
						}
						return gi < gj
					}
					if out[i].Status != out[j].Status {
						return out[i].Status == "connected"
					}
					return out[i].Hostname < out[j].Hostname
				})
			}
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				rows = out
				metas = nextMetas
				list.Refresh()
			})
		}()
	}

	refreshBtn.OnTapped = refresh
	filter.OnSubmitted = func(string) { refresh() }

	list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(rows) {
			return
		}
		actions.setSelected(rows[id])
	}

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()

	top := container.NewBorder(nil, nil, nil, refreshBtn, filter)
	body := container.NewBorder(top, nil, nil, nil, container.NewScroll(list))
	return container.NewPadded(widget.NewCard("Clients", "Search/select clients. Right-click connected clients for group, note, and sleep controls.", body))
}

type clientListItem struct {
	widget.BaseWidget
	label       *widget.Label
	index       int
	onSecondary func(index int, absPos fyne.Position)
}

func newClientListItem() *clientListItem {
	it := &clientListItem{label: widget.NewLabel("")}
	it.ExtendBaseWidget(it)
	return it
}

func (i *clientListItem) set(index int, text string) {
	i.index = index
	i.label.SetText(text)
}

func (i *clientListItem) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(i.label)
}

func (i *clientListItem) TappedSecondary(ev *fyne.PointEvent) {
	if i.onSecondary != nil {
		i.onSecondary(i.index, ev.AbsolutePosition)
	}
}

func showClientContextMenu(w fyne.Window, sum users.ClientSummary, meta data.ClientMeta, refresh func(), absPos fyne.Position) {
	metaItem := fyne.NewMenuItem("Set Group / Note...", func() {
		showClientMetaDialog(w, sum, meta, refresh)
	})

	items := []*fyne.MenuItem{metaItem}
	if sum.Status == "connected" {
		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Sleep Minutes...", func() { showClientSleepMinutesDialog(w, sum, refresh) }),
			fyne.NewMenuItem("Sleep Until...", func() { showClientSleepUntilDialog(w, sum, refresh) }),
			fyne.NewMenuItem("Clear Sleep", func() {
				if err := applyClientSleep(sum, ""); err != nil {
					dialog.ShowError(err, w)
					return
				}
				if refresh != nil {
					refresh()
				}
			}),
		)
	}

	popup := widget.NewPopUpMenu(fyne.NewMenu("", items...), w.Canvas())
	popup.ShowAtPosition(absPos)
}

func showClientMetaDialog(w fyne.Window, sum users.ClientSummary, meta data.ClientMeta, refresh func()) {
	group := widget.NewEntry()
	group.SetText(meta.Group)
	note := widget.NewMultiLineEntry()
	note.SetText(meta.Note)
	note.SetMinRowsVisible(5)

	form := widget.NewForm(
		widget.NewFormItem("Group", group),
		widget.NewFormItem("Note", note),
	)
	d := dialog.NewCustomConfirm("Client Meta", "Save", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		if _, err := data.UpsertClientMeta(sum.Fingerprint, group.Text, note.Text); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if refresh != nil {
			refresh()
		}
	}, w)
	d.Resize(fyne.NewSize(520, 360))
	d.Show()
}

func showClientSleepMinutesDialog(w fyne.Window, sum users.ClientSummary, refresh func()) {
	minutes := widget.NewEntry()
	minutes.SetPlaceHolder("minutes")
	d := dialog.NewCustomConfirm("Sleep Client", "Sleep", "Cancel", minutes, func(ok bool) {
		if !ok {
			return
		}
		mins, err := strconv.Atoi(strings.TrimSpace(minutes.Text))
		if err != nil || mins <= 0 || mins > 365*24*60 {
			dialog.ShowError(errors.New("minutes out of range"), w)
			return
		}
		until := time.Now().Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)
		if err := applyClientSleep(sum, until); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if refresh != nil {
			refresh()
		}
	}, w)
	d.Show()
}

func showClientSleepUntilDialog(w fyne.Window, sum users.ClientSummary, refresh func()) {
	untilEntry := widget.NewEntry()
	untilEntry.SetPlaceHolder("RFC3339, e.g. 2026-04-20T12:00:00Z")
	if comm, _, err := data.GetClientCommSettings(sum.Fingerprint); err == nil {
		untilEntry.SetText(comm.SleepUntil)
	}
	d := dialog.NewCustomConfirm("Sleep Until", "Apply", "Cancel", untilEntry, func(ok bool) {
		if !ok {
			return
		}
		until := strings.TrimSpace(untilEntry.Text)
		if until == "" {
			return
		}
		if _, err := time.Parse(time.RFC3339, until); err != nil {
			dialog.ShowError(errors.New("until must be RFC3339"), w)
			return
		}
		if err := applyClientSleep(sum, until); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if refresh != nil {
			refresh()
		}
	}, w)
	d.Show()
}

func applyClientSleep(sum users.ClientSummary, until string) error {
	comm, _, _ := data.GetClientCommSettings(sum.Fingerprint)
	comm.SleepUntil = strings.TrimSpace(until)
	if _, err := data.UpsertClientCommSettings(sum.Fingerprint, comm); err != nil {
		return err
	}
	conn, ok := users.GetClientConnByFingerprint(sum.Fingerprint)
	if !ok {
		return errors.New("client not connected")
	}
	_, _, _ = conn.SendRequest("sleep-until@rssh", false, []byte(mustJSON(map[string]any{"until": comm.SleepUntil})))
	if comm.SleepUntil != "" {
		_ = conn.Close()
	}
	return nil
}

type shellSession struct {
	ch ssh.Channel
}

func (s *shellSession) Read(p []byte) (int, error)  { return s.ch.Read(p) }
func (s *shellSession) Write(p []byte) (int, error) { return s.ch.Write(p) }
func (s *shellSession) Close() error                { return s.ch.Close() }

func openShell(conn *ssh.ServerConn, cols, rows uint32) (*shellSession, error) {
	ch, reqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		return nil, err
	}
	go ssh.DiscardRequests(reqs)

	pty := internal.PtyReq{
		Term:    "xterm-256color",
		Columns: cols,
		Rows:    rows,
		Width:   0,
		Height:  0,
		Modes:   "",
	}
	if _, err := ch.SendRequest("pty-req", true, ssh.Marshal(pty)); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if _, err := ch.SendRequest("shell", true, ssh.Marshal(internal.ShellStruct{Cmd: ""})); err != nil {
		_ = ch.Close()
		return nil, err
	}
	return &shellSession{ch: ch}, nil
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

	const maxOut = 1 << 20
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(ch, maxOut))
		done <- string(b)
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case out := <-done:
		return out, nil
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func runAsync(w fyne.Window, title, message string, fn func() error, done func()) {
	p := dialog.NewProgressInfinite(title, message, w)
	p.Show()
	go func() {
		err := fn()
		fyne.Do(func() {
			p.Hide()
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

func showTextPreviewDialog(w fyne.Window, title, text string) {
	view := widget.NewMultiLineEntry()
	view.Wrapping = fyne.TextWrapOff
	view.Scroll = fyne.ScrollBoth
	view.TextStyle = fyne.TextStyle{Monospace: true}
	view.SetText(text)
	view.SetMinRowsVisible(24)

	copyBtn := widget.NewButtonWithIcon("Copy All", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(view.Text)
	})
	body := container.NewBorder(nil, container.NewHBox(copyBtn, layout.NewSpacer()), nil, nil, view)
	d := dialog.NewCustom(title, "Close", body, w)
	d.Resize(fyne.NewSize(860, 620))
	d.Show()
}

type sftpEntry struct {
	Name    string
	Path    string
	Size    int64
	Mode    string
	UID     uint32
	GID     uint32
	ModTime time.Time
	IsDir   bool
}

type sftpFileMeta struct {
	Size    int64
	Mode    string
	ModTime time.Time
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
			if dir == "/" {
				p = "/" + fi.Name()
			}
			out = append(out, sftpEntry{
				Name:    fi.Name(),
				Path:    p,
				Size:    fi.Size(),
				Mode:    fi.Mode().String(),
				UID:     0,
				GID:     0,
				ModTime: fi.ModTime(),
				IsDir:   fi.IsDir(),
			})
		}
		return nil
	})
	return out, err
}

func sftpReadFileChunk(conn *ssh.ServerConn, filePath string, offset int64, maxBytes int64) (content string, encoding string, truncated bool, meta sftpFileMeta, err error) {
	if offset < 0 {
		offset = 0
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
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
	return base64.StdEncoding.EncodeToString(out), "base64", truncated, meta, nil
}

func sftpReadFileChunkCached(fingerprint string, conn *ssh.ServerConn, entry sftpEntry, offset int64, maxBytes int64) (content string, encoding string, truncated bool, meta sftpFileMeta, err error) {
	if offset < 0 {
		offset = 0
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	if maxBytes > 1024*1024 {
		maxBytes = 1024 * 1024
	}

	meta = sftpFileMeta{
		Size:    entry.Size,
		Mode:    entry.Mode,
		ModTime: entry.ModTime,
	}
	key := data.FileCacheKey(fingerprint, entry.Path, offset, maxBytes, meta.Size, meta.ModTime.UnixNano())
	if cached, ok, cacheErr := data.GetFileCache(key); cacheErr == nil && ok {
		return cached.Content, cached.Encoding, cached.Truncated, sftpFileMeta{
			Size:    cached.Size,
			Mode:    cached.Mode,
			ModTime: time.Unix(0, cached.ModUnixNano),
		}, nil
	}

	content, encoding, truncated, meta, err = sftpReadFileChunk(conn, entry.Path, offset, maxBytes)
	if err != nil {
		return "", "", false, sftpFileMeta{}, err
	}
	_ = data.UpsertFileCache(data.FileCache{
		Key:         key,
		Fingerprint: fingerprint,
		Path:        entry.Path,
		Offset:      offset,
		MaxBytes:    maxBytes,
		Size:        meta.Size,
		ModUnixNano: meta.ModTime.UnixNano(),
		Encoding:    encoding,
		Content:     content,
		Truncated:   truncated,
		Mode:        meta.Mode,
	})
	return content, encoding, truncated, meta, nil
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
	return internal.RemoteForwardRequest{BindAddr: host, BindPort: uint32(p)}, nil
}

type fileListItem struct {
	widget.BaseWidget
	label       *widget.Label
	index       int
	onSecondary func(index int, absPos fyne.Position)
	onDouble    func(index int)
}

func newFileListItem() *fileListItem {
	it := &fileListItem{label: widget.NewLabel("")}
	it.ExtendBaseWidget(it)
	return it
}

func (i *fileListItem) set(index int, text string) {
	i.index = index
	i.label.SetText(text)
}

func (i *fileListItem) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(i.label)
}

func (i *fileListItem) TappedSecondary(ev *fyne.PointEvent) {
	if i.onSecondary != nil {
		i.onSecondary(i.index, ev.AbsolutePosition)
	}
}

func (i *fileListItem) DoubleTapped(_ *fyne.PointEvent) {
	if i.onDouble != nil {
		i.onDouble(i.index)
	}
}

func showFileContextMenu(
	w fyne.Window,
	ctx func() (conn *ssh.ServerConn, curDir string, entry sftpEntry, ok bool, err error),
	refresh func(),
	absPos fyne.Position,
) {
	conn, curDir, entry, ok, err := ctx()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	if !ok {
		return
	}

	targetDir := curDir
	if entry.IsDir {
		targetDir = entry.Path
	}
	fingerprint := strings.TrimSpace(conn.Permissions.Extensions["pubkey-fp"])

	refreshItem := fyne.NewMenuItem("Refresh", func() { refresh() })

	uploadItem := fyne.NewMenuItem("Upload Here…", func() {
		dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if rc == nil {
				return
			}
			defer rc.Close()

			localPath := rc.URI().Path()
			remotePath := path.Join(targetDir, filepath.Base(localPath))
			runAsync(w, "Upload", "Uploading…", func() error {
				if err := sftpUploadLocalFile(conn, targetDir, localPath); err != nil {
					return err
				}
				return data.DeleteFileCacheForPath(fingerprint, remotePath)
			}, refresh)
		}, w)
	})

	newFolderItem := fyne.NewMenuItem("New Folder…", func() {
		d := dialog.NewEntryDialog("New Folder", "Folder name", func(name string) {
			name = strings.TrimSpace(name)
			if name == "" {
				return
			}
			p := path.Join(targetDir, name)
			runAsync(w, "New Folder", "Creating…", func() error { return sftpMkdir(conn, p) }, refresh)
		}, w)
		d.Show()
	})

	downloadItem := fyne.NewMenuItem("Download…", func() {
		if entry.IsDir {
			dialog.ShowError(errors.New("download of directories is not supported yet"), w)
			return
		}
		dialog.ShowFolderOpen(func(dir fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if dir == nil {
				return
			}
			target := filepath.Join(dir.Path(), entry.Name)
			start := func() {
				runAsync(w, "Download", "Downloading…", func() error {
					return sftpDownloadFile(conn, entry.Path, target)
				}, func() {
					dialog.ShowInformation("Download", "Saved to "+target, w)
				})
			}
			if _, err := os.Stat(target); err == nil {
				dialog.ShowConfirm("Overwrite?", "File already exists:\n\n"+target+"\n\nOverwrite it?", func(ok bool) {
					if ok {
						start()
					}
				}, w)
				return
			} else if !os.IsNotExist(err) {
				dialog.ShowError(err, w)
				return
			}
			start()
		}, w)
	})

	editItem := fyne.NewMenuItem("Edit…", func() {
		if entry.IsDir {
			return
		}
		content, truncated, err := sftpReadTextFile(conn, entry.Path, 512*1024)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if truncated {
			dialog.ShowError(errors.New("file too large to edit (truncated)"), w)
			return
		}

		editor := widget.NewMultiLineEntry()
		editor.SetText(content)
		editor.SetMinRowsVisible(24)

		d := dialog.NewCustomConfirm("Edit: "+entry.Path, "Save", "Cancel", container.NewScroll(editor), func(ok bool) {
			if !ok {
				return
			}
			runAsync(w, "Save File", "Saving…", func() error {
				if err := sftpWriteTextFile(conn, entry.Path, editor.Text); err != nil {
					return err
				}
				return data.DeleteFileCacheForPath(fingerprint, entry.Path)
			}, refresh)
		}, w)
		d.Resize(fyne.NewSize(820, 560))
		d.Show()
	})

	renameItem := fyne.NewMenuItem("Rename…", func() {
		d := dialog.NewEntryDialog("Rename", "New name", func(name string) {
			name = strings.TrimSpace(name)
			if name == "" {
				return
			}
			newPath := path.Join(path.Dir(entry.Path), name)
			runAsync(w, "Rename", "Renaming…", func() error {
				if err := sftpRename(conn, entry.Path, newPath); err != nil {
					return err
				}
				if err := data.DeleteFileCacheForPath(fingerprint, entry.Path); err != nil {
					return err
				}
				return data.DeleteFileCacheForPath(fingerprint, newPath)
			}, refresh)
		}, w)
		d.SetText(entry.Name)
		d.Show()
	})

	deleteItem := fyne.NewMenuItem("Delete…", func() {
		if entry.IsDir {
			dialog.ShowConfirm("Delete Folder", "Delete folder recursively?\n\n"+entry.Path, func(ok bool) {
				if !ok {
					return
				}
				runAsync(w, "Delete", "Deleting…", func() error {
					if err := sftpRemove(conn, entry.Path, true); err != nil {
						return err
					}
					return data.DeleteFileCacheForPath(fingerprint, entry.Path)
				}, refresh)
			}, w)
			return
		}
		dialog.ShowConfirm("Delete File", "Delete file?\n\n"+entry.Path, func(ok bool) {
			if !ok {
				return
			}
			runAsync(w, "Delete", "Deleting…", func() error {
				if err := sftpRemove(conn, entry.Path, false); err != nil {
					return err
				}
				return data.DeleteFileCacheForPath(fingerprint, entry.Path)
			}, refresh)
		}, w)
	})

	menu := fyne.NewMenu("",
		refreshItem,
		uploadItem,
		newFolderItem,
		fyne.NewMenuItemSeparator(),
		downloadItem,
		editItem,
		renameItem,
		deleteItem,
	)
	popup := widget.NewPopUpMenu(menu, w.Canvas())
	popup.ShowAtPosition(absPos)
}

func sftpMkdir(conn *ssh.ServerConn, p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("path is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error { return c.MkdirAll(p) })
}

func sftpRename(conn *ssh.ServerConn, from, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return errors.New("from/to is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error { return c.Rename(from, to) })
}

func sftpRemove(conn *ssh.ServerConn, p string, recursive bool) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("path is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error {
		if recursive {
			return c.RemoveAll(p)
		}
		return c.Remove(p)
	})
}

func sftpDownloadFile(conn *ssh.ServerConn, remotePath, localPath string) error {
	remotePath = strings.TrimSpace(remotePath)
	localPath = strings.TrimSpace(localPath)
	if remotePath == "" || localPath == "" {
		return errors.New("path is empty")
	}
	return withSFTP(conn, func(c *sftp.Client) error {
		src, err := c.Open(remotePath)
		if err != nil {
			return err
		}
		defer src.Close()

		dst, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer dst.Close()

		_, err = io.Copy(dst, src)
		return err
	})
}

func sftpUploadLocalFile(conn *ssh.ServerConn, remoteDir, localPath string) error {
	remoteDir = strings.TrimSpace(remoteDir)
	localPath = strings.TrimSpace(localPath)
	if remoteDir == "" {
		remoteDir = "/"
	}
	if localPath == "" {
		return errors.New("local path is empty")
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	name := filepath.Base(localPath)
	remotePath := path.Join(remoteDir, name)
	return withSFTP(conn, func(c *sftp.Client) error {
		dst, err := c.Create(remotePath)
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, f)
		return err
	})
}

func sftpReadTextFile(conn *ssh.ServerConn, filePath string, maxBytes int64) (content string, truncated bool, err error) {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
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
	if !utf8.Valid(out) {
		return "", false, errors.New("file is not valid utf-8 text")
	}
	return string(out), truncated, nil
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

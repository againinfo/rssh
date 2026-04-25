package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/crypto/ssh"

	"rssh/internal"
	"rssh/internal/server/multiplexer"
	"rssh/internal/server/socksproxy"
	"rssh/internal/server/tunnels"
	"rssh/internal/server/users"
)

func newNetworkLaunchPage(a fyne.App, parent fyne.Window, getSelectedID func() string, tunnelMgr *tunnels.Manager, socksMgr *socksproxy.Manager) fyne.CanvasObject {
	openBtn := widget.NewButtonWithIcon("Open Network Tools…", theme.WindowMaximizeIcon(), func() {
		openNetworkWindow(a, parent, getSelectedID, tunnelMgr, socksMgr)
	})
	openBtn.Importance = widget.HighImportance

	info := widget.NewLabel("Forwards, Tunnels, and SOCKS5 are consolidated into a dedicated window.")
	info.Wrapping = fyne.TextWrapWord

	return container.NewPadded(widget.NewCard("Network", "Open the network tools window.", container.NewVBox(
		info,
		openBtn,
	)))
}

func openNetworkWindow(a fyne.App, parent fyne.Window, getSelectedID func() string, tunnelMgr *tunnels.Manager, socksMgr *socksproxy.Manager) {
	w := a.NewWindow("Network Tools")
	w.Resize(fyne.NewSize(980, 680))

	clientIDEntry := widget.NewEntry()
	clientIDEntry.SetPlaceHolder("Client fingerprint (required for client-side actions)")
	if getSelectedID != nil {
		if id := strings.TrimSpace(getSelectedID()); id != "" && id != "None" {
			clientIDEntry.SetText(id)
		}
	}

	getConn := func() (*ssh.ServerConn, error) {
		id := strings.TrimSpace(clientIDEntry.Text)
		if id == "" {
			return nil, errors.New("client id is empty")
		}
		conn, ok := users.GetClientConnByFingerprint(id)
		if !ok {
			return nil, errors.New("client not connected")
		}
		return conn, nil
	}

	connectedSelect := widget.NewSelect([]string{}, func(s string) {
		if strings.TrimSpace(s) != "" {
			clientIDEntry.SetText(strings.TrimSpace(s))
		}
	})
	connectedSelect.PlaceHolder = "Select connected client…"

	refreshClients := func() {
		rows, err := users.ListConnectedClients("")
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		opts := make([]string, 0, len(rows))
		for _, r := range rows {
			opts = append(opts, r.ID)
		}
		connectedSelect.Options = opts
		connectedSelect.Refresh()
	}

	top := container.NewVBox(
		widget.NewCard("Target", "Pick the client to use for client-side operations.", container.NewVBox(
			container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshClients), clientIDEntry),
			connectedSelect,
		)),
	)

	// ---------- Forwards ----------
	serverListenAddr := widget.NewEntry()
	serverListenAddr.SetPlaceHolder("e.g. 0.0.0.0:2222")
	serverListenOn := widget.NewButton("Start", func() {
		if multiplexer.ServerMultiplexer == nil {
			dialog.ShowError(errors.New("server not running"), w)
			return
		}
		addr := strings.TrimSpace(serverListenAddr.Text)
		if addr == "" {
			return
		}
		runAsync(w, "Server Listener", "Starting…", func() error {
			return multiplexer.ServerMultiplexer.StartListener("tcp", addr)
		}, nil)
	})
	serverListenOff := widget.NewButton("Stop", func() {
		if multiplexer.ServerMultiplexer == nil {
			dialog.ShowError(errors.New("server not running"), w)
			return
		}
		addr := strings.TrimSpace(serverListenAddr.Text)
		if addr == "" {
			return
		}
		runAsync(w, "Server Listener", "Stopping…", func() error {
			return multiplexer.ServerMultiplexer.StopListener(addr)
		}, nil)
	})

	clientForwardAddr := widget.NewEntry()
	clientForwardAddr.SetPlaceHolder("e.g. 127.0.0.1:3389")
	clientForwardOn := widget.NewButton("Forward On", func() {
		conn, err := getConn()
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
		runAsync(w, "Client Forward", "Requesting…", func() error {
			ok, msg, err := conn.SendRequest("tcpip-forward", true, ssh.Marshal(&req))
			if err != nil || !ok {
				return fmt.Errorf("request failed: %v %s", err, strings.TrimSpace(string(msg)))
			}
			return nil
		}, nil)
	})
	clientForwardOff := widget.NewButton("Forward Off", func() {
		conn, err := getConn()
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
		runAsync(w, "Client Forward", "Cancelling…", func() error {
			ok, msg, err := conn.SendRequest("cancel-tcpip-forward", true, ssh.Marshal(&req))
			if err != nil || !ok {
				return fmt.Errorf("request failed: %v %s", err, strings.TrimSpace(string(msg)))
			}
			return nil
		}, nil)
	})

	forwardsTab := container.NewPadded(container.NewVBox(
		widget.NewCard("Server Listeners", "Start/stop listeners on the RSSH server.", container.NewVBox(
			container.NewBorder(nil, nil, nil, container.NewHBox(serverListenOn, serverListenOff), serverListenAddr),
		)),
		widget.NewCard("Client Remote Forward", "Request tcpip-forward on the selected client.", container.NewVBox(
			container.NewBorder(nil, nil, nil, container.NewHBox(clientForwardOn, clientForwardOff), clientForwardAddr),
		)),
	))

	// ---------- Tunnels ----------
	allowText := widget.NewMultiLineEntry()
	allowText.Disable()
	allowText.SetMinRowsVisible(6)

	allowRefresh := widget.NewButtonWithIcon("Refresh Allowlist", theme.ViewRefreshIcon(), func() {
		entries := tunnels.AllowlistCIDRs()
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(e.String())
			sb.WriteString("\n")
		}
		allowText.SetText(sb.String())
	})

	tunnelListenPort := widget.NewEntry()
	tunnelListenPort.SetPlaceHolder("local listen port (e.g. 1081)")
	tunnelTarget := widget.NewEntry()
	tunnelTarget.SetPlaceHolder("target host:port (e.g. 10.0.0.5:22)")

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
	tunnelList.OnSelected = func(id widget.ListItemID) { selectedTunnel = id }
	refreshTunnels := func() {
		activeTunnels = tunnelMgr.List()
		selectedTunnel = -1
		tunnelList.Refresh()
	}

	tunnelCreate := widget.NewButton("Create Tunnel", func() {
		id := strings.TrimSpace(clientIDEntry.Text)
		port := strings.TrimSpace(tunnelListenPort.Text)
		target := strings.TrimSpace(tunnelTarget.Text)
		if id == "" || port == "" || target == "" {
			return
		}
		runAsync(w, "Tunnel", "Creating…", func() error {
			_, err := tunnelMgr.Create(id, port, target)
			return err
		}, refreshTunnels)
	})

	tunnelClose := widget.NewButton("Close Selected", func() {
		if selectedTunnel < 0 || selectedTunnel >= len(activeTunnels) {
			return
		}
		id := activeTunnels[selectedTunnel].ID
		runAsync(w, "Tunnel", "Closing…", func() error {
			return tunnelMgr.Close(id)
		}, refreshTunnels)
	})

	tunnelsTab := container.NewPadded(container.NewVBox(
		widget.NewCard("Allowlist", "Loaded from datadir and/or environment.", container.NewVBox(
			container.NewHBox(allowRefresh, layout.NewSpacer()),
			container.NewScroll(allowText),
		)),
		widget.NewCard("Active Tunnels", "Local listener that dials to target via the selected client.", container.NewVBox(
			container.NewBorder(nil, nil, nil, tunnelCreate, container.NewGridWithColumns(2, tunnelListenPort, tunnelTarget)),
			container.NewHBox(tunnelClose, widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshTunnels), layout.NewSpacer()),
			container.NewScroll(tunnelList),
		)),
	))

	// ---------- SOCKS5 ----------
	socksBind := widget.NewEntry()
	socksBind.SetText("127.0.0.1")
	socksPort := widget.NewEntry()
	socksPort.SetPlaceHolder("port (e.g. 1080)")

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
	socksList.OnSelected = func(id widget.ListItemID) { selectedSOCKS = id }
	refreshSOCKS := func() {
		activeSOCKS = socksMgr.List()
		selectedSOCKS = -1
		socksList.Refresh()
	}

	socksCreate := widget.NewButton("Create SOCKS", func() {
		id := strings.TrimSpace(clientIDEntry.Text)
		if id == "" {
			return
		}
		runAsync(w, "SOCKS", "Creating…", func() error {
			_, err := socksMgr.Create(id, strings.TrimSpace(socksBind.Text), strings.TrimSpace(socksPort.Text))
			return err
		}, refreshSOCKS)
	})
	socksClose := widget.NewButton("Close Selected", func() {
		if selectedSOCKS < 0 || selectedSOCKS >= len(activeSOCKS) {
			return
		}
		id := activeSOCKS[selectedSOCKS].ID
		runAsync(w, "SOCKS", "Closing…", func() error {
			return socksMgr.Close(id)
		}, refreshSOCKS)
	})

	socksTab := container.NewPadded(container.NewVBox(
		widget.NewCard("Create", "SOCKS5 listener that dials out through the selected client (CONNECT only).", container.NewVBox(
			container.NewBorder(nil, nil, nil, socksCreate, container.NewGridWithColumns(2, socksBind, socksPort)),
		)),
		widget.NewCard("Active SOCKS", "", container.NewVBox(
			container.NewHBox(socksClose, widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshSOCKS), layout.NewSpacer()),
			container.NewScroll(socksList),
		)),
	))

	// Refresh counts in background so the window feels alive.
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				refreshTunnels()
				refreshSOCKS()
			})
		}
	}()

	tabs := container.NewAppTabs(
		container.NewTabItem("Forwards", forwardsTab),
		container.NewTabItem("Tunnels", tunnelsTab),
		container.NewTabItem("SOCKS5", socksTab),
	)

	w.SetContent(container.NewBorder(top, nil, nil, nil, tabs))

	allowRefresh.OnTapped()
	refreshClients()
	refreshTunnels()
	refreshSOCKS()

	w.Show()
}

// Helper to satisfy lints when the file is compiled without old code paths.
var _ = internal.Version

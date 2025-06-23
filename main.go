package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
)

var (
	//go:embed icon/on.png
	iconOn []byte
	//go:embed icon/off.png
	iconOff []byte
	//go:embed icon/on-with-exit-node.png
	iconOnExitNodeActive []byte
)

var (
	mu   sync.RWMutex
	myIP string
)

func main() {
	systray.Run(onReady, nil)
}

func executable(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func doConnectionControl(m *systray.MenuItem, verb string) {
	for {
		if _, ok := <-m.ClickedCh; !ok {
			break
		}
		b, err := exec.Command("pkexec", "tailscale", verb).CombinedOutput()
		if err != nil {
			beeep.Notify(
				"Tailscale",
				string(b),
				"",
			)
		}
	}
}

func onReady() {
	systray.SetIcon(iconOff)

	mConnect := systray.AddMenuItem("Connect", "")
	mConnect.Enable()
	mDisconnect := systray.AddMenuItem("Disconnect", "")
	mDisconnect.Disable()

	// Helper to update DNS routing menu state
	updateDNSRoutingMenu := func(m *systray.MenuItem) {
		enabled, err := getTailscaleDNSStatus()
		if err != nil {
			m.SetTitle("Use Tailscale DNS (unknown)")
			m.Uncheck()
			return
		}
		m.SetTitle("Use Tailscale DNS")
		if enabled {
			m.Check()
		} else {
			m.Uncheck()
		}
	}

	if executable("pkexec") {
		go doConnectionControl(mConnect, "up")
		go doConnectionControl(mDisconnect, "down")
	} else {
		mConnect.Hide()
		mDisconnect.Hide()
	}

	systray.AddSeparator()

	mThisDevice := systray.AddMenuItem("This device:", "")
	go func(mThisDevice *systray.MenuItem) {
		for {
			_, ok := <-mThisDevice.ClickedCh
			if !ok {
				break
			}
			mu.RLock()
			if myIP == "" {
				mu.RUnlock()
				continue
			}
			err := clipboard.WriteAll(myIP)
			if err == nil {
				beeep.Notify(
					"This device",
					fmt.Sprintf("Copy the IP address (%s) to the Clipboard", myIP),
					"",
				)
			}
			mu.RUnlock()
		}
	}(mThisDevice)

	mNetworkDevices := systray.AddMenuItem("Network Devices", "")
	mMyDevices := mNetworkDevices.AddSubMenuItem("My Devices", "")
	mTailscaleServices := mNetworkDevices.AddSubMenuItem("Tailscale Services", "")

	// Exit Node submenu
	mExitNode := systray.AddMenuItem("Exit Node", "Configure exit node")
	mDisableExitNode := mExitNode.AddSubMenuItem("Disable Exit Node", "Disable all exit nodes")
	mDisableExitNode.Hide()
	exitNodeItems := map[string]*systray.MenuItem{}
	var currentExitNode string

	// DNS Routing menu (moved here, after Exit Node)
	mDNSRouting := systray.AddMenuItemCheckbox("Use Tailscale DNS", "Enable or disable Tailscale DNS routing", false)
	go func() {
		for {
			_, ok := <-mDNSRouting.ClickedCh
			if !ok {
				break
			}
			go func() {
				enable := !mDNSRouting.Checked()
				var arg string
				if enable {
					arg = "--accept-dns=true"
				} else {
					arg = "--accept-dns=false"
				}
				b, err := exec.Command("tailscale", "set", arg).CombinedOutput()
				if err != nil {
					beeep.Notify(
						"Tailscale",
						fmt.Sprintf("Failed to set DNS routing: %s", string(b)),
						"",
					)
				}
				// Update the checkbox state after toggling
				updateDNSRoutingMenu(mDNSRouting)
			}()
		}
	}()

	// Tailscale Routes menu
	mRoutes := systray.AddMenuItemCheckbox("Use Tailscale Routes", "Enable or disable Tailscale subnet routes", false)
	updateRoutesMenu := func(m *systray.MenuItem) {
		enabled, err := getTailscaleRoutesStatus()
		if err != nil {
			m.SetTitle("Use Tailscale Routes (unknown)")
			m.Uncheck()
			return
		}
		m.SetTitle("Use Tailscale Routes")
		if enabled {
			m.Check()
		} else {
			m.Uncheck()
		}
	}
	go func() {
		for {
			_, ok := <-mRoutes.ClickedCh
			if !ok {
				break
			}
			go func() {
				enable := !mRoutes.Checked()
				var arg string
				if enable {
					arg = "--accept-routes=true"
				} else {
					arg = "--accept-routes=false"
				}
				b, err := exec.Command("tailscale", "set", arg).CombinedOutput()
				if err != nil {
					beeep.Notify(
						"Tailscale",
						fmt.Sprintf("Failed to set routes: %s", string(b)),
						"",
					)
				}
				// Update the checkbox state after toggling
				updateRoutesMenu(mRoutes)
			}()
		}
	}()

	systray.AddSeparator()
	mAdminConsole := systray.AddMenuItem("Admin Console...", "")
	go func() {
		for {
			_, ok := <-mAdminConsole.ClickedCh
			if !ok {
				break
			}
			openBrowser("https://login.tailscale.com/admin/machines")
		}
	}()

	systray.AddSeparator()

	mExit := systray.AddMenuItem("Exit", "")
	go func() {
		<-mExit.ClickedCh
		systray.Quit()
	}()

	// --- UI update logic extracted to a function ---

	updateUI := func() {
		type Item struct {
			menu  *systray.MenuItem
			title string
			ip    string
			found bool
		}
		items := map[string]*Item{}

		enabled := false
		setDisconnected := func() {
			if enabled {
				systray.SetTooltip("Tailscale: Disconnected")
				mConnect.Enable()
				mDisconnect.Disable()
				systray.SetIcon(iconOff)
				enabled = false
			}
		}

		var doUpdate func()
		doUpdate = func() {
			rawStatus, err := exec.Command("tailscale", "status", "--json").Output()
			if err != nil {
				setDisconnected()
				return
			}

			status := new(Status)
			if err := json.Unmarshal(rawStatus, status); err != nil {
				setDisconnected()
				return
			}

			mu.Lock()
			if len(status.Self.TailscaleIPs) != 0 {
				myIP = status.Self.TailscaleIPs[0]
			}
			mu.Unlock()

			if status.TailscaleUp && !enabled {
				systray.SetTooltip("Tailscale: Connected")
				mConnect.Disable()
				mDisconnect.Enable()
				// Set icon based on exit node status
				if hasActiveExitNode(status) {
					systray.SetIcon(iconOnExitNodeActive)
				} else {
					systray.SetIcon(iconOn)
				}
				enabled = true
			} else if !status.TailscaleUp && enabled {
				setDisconnected()
			}

			for _, v := range items {
				v.found = false
			}

			// Update icon if already enabled and exit node status changes
			if enabled {
				if hasActiveExitNode(status) {
					systray.SetIcon(iconOnExitNodeActive)
				} else {
					systray.SetIcon(iconOn)
				}
			}

			mThisDevice.SetTitle(fmt.Sprintf("This device: %s (%s)", status.Self.DisplayName.String(), myIP))

			// --- Exit Node submenu logic ---
			// Hide all exit node items by default
			for _, item := range exitNodeItems {
				item.Hide()
			}
			mDisableExitNode.Hide()
			currentExitNode = ""
			exitNodeCandidates := map[string]*systray.MenuItem{}

			for _, peer := range status.Peers {
				ip := peer.TailscaleIPs[0]
				peerName := peer.DisplayName
				title := peerName.String()

				// Exit Node submenu: check for ExitNodeOption
				exitNodeOption := peer.ExitNodeOption
				exitNodeActive := peer.ExitNode

				if exitNodeOption {
					var item *systray.MenuItem
					if old, ok := exitNodeItems[title]; ok {
						item = old
						item.Show()
					} else {
						item = mExitNode.AddSubMenuItemCheckbox(title, fmt.Sprintf("Use %s as exit node", title), false)
						exitNodeItems[title] = item
						go func(item *systray.MenuItem, nodeName string) {
							for {
								_, ok := <-item.ClickedCh
								if !ok {
									break
								}
								// Set this node as exit node
								go func() {
									b, err := exec.Command("tailscale", "set", "--exit-node", nodeName).CombinedOutput()
									if err != nil {
										beeep.Notify(
											"Tailscale",
											fmt.Sprintf("Failed to set exit node: %s", string(b)),
											"",
										)
									}
									// Instantly update the list of nodes in the submenu
									// by calling the UI update function directly
									doUpdate()
								}()
							}
						}(item, title)
					}
					if exitNodeActive {
						item.Check()
					} else {
						item.Uncheck()
					}
					if exitNodeActive {
						currentExitNode = title
					}
					item.Show()
					exitNodeCandidates[title] = item
				}

				var sub *systray.MenuItem
				switch peerName.(type) {
				case DNSName:
					sub = mMyDevices
				case HostName:
					sub = mTailscaleServices
				}

				if item, ok := items[title]; ok {
					item.found = true
				} else {
					items[title] = &Item{
						menu:  sub.AddSubMenuItem(title, title),
						title: title,
						ip:    ip,
						found: true,
					}
					go func(item *Item) {
						// TODO fix race condition
						for {
							_, ok := <-item.menu.ClickedCh
							if !ok {
								break
							}
							err := clipboard.WriteAll(item.ip)
							if err != nil {
								beeep.Notify(
									"Tailscale",
									err.Error(),
									"",
								)
								return
							}
							beeep.Notify(
								item.title,
								fmt.Sprintf("Copy the IP address (%s) to the Clipboard", item.ip),
								"",
							)
						}
					}(items[title])
				}
			}

			// Show "Disable Exit Node" if any exit node is active
			if currentExitNode != "" {
				mDisableExitNode.Show()
			} else {
				mDisableExitNode.Hide()
			}

			for k, v := range items {
				if !v.found {
					// TODO fix race condition
					v.menu.Hide()
					delete(items, k)
				}
			}
		}

		// Run the update loop
		go func() {
			for {
				doUpdate()
				updateDNSRoutingMenu(mDNSRouting)
				time.Sleep(10 * time.Second)
			}
		}()
	}

	updateUI()
	// Initial DNS routing menu state
	updateDNSRoutingMenu(mDNSRouting)
	// Initial routes menu state
	updateRoutesMenu(mRoutes)

	// Exit Node: disable handler
	go func() {
		for {
			_, ok := <-mDisableExitNode.ClickedCh
			if !ok {
				break
			}
			go func() {
				b, err := exec.Command("tailscale", "set", "--exit-node", "").CombinedOutput()
				if err != nil {
					beeep.Notify(
						"Tailscale",
						fmt.Sprintf("Failed to disable exit node: %s", string(b)),
						"",
					)
				}
			}()
		}
	}()
}

func getTailscaleDNSStatus() (bool, error) {
	out, err := exec.Command("tailscale", "dns", "status").CombinedOutput()
	if err != nil {
		return false, err
	}
	reEnabled := regexp.MustCompile(`^Tailscale DNS:\s*enabled`)
	reDisabled := regexp.MustCompile(`^Tailscale DNS:\s*disabled`)
	lines := string(out)
	for _, line := range splitLines(lines) {
		if reEnabled.MatchString(line) {
			return true, nil
		}
		if reDisabled.MatchString(line) {
			return false, nil
		}
	}
	return false, nil
}

// getTailscaleRoutesStatus returns true if --accept-routes is enabled, false otherwise.
func getTailscaleRoutesStatus() (bool, error) {
	out, err := exec.Command("tailscale", "status", "--json").CombinedOutput()
	if err != nil {
		return false, err
	}
	// Look for the health message indicating routes are disabled
	var status struct {
		Health []string `json:"Health"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, err
	}
	for _, msg := range status.Health {
		// Check if the message contains "--accept-routes is false"
		if containsAcceptRoutesFalse(msg) {
			return false, nil
		}
	}
	return true, nil
}

// containsAcceptRoutesFalse returns true if the message contains "--accept-routes is false"
func containsAcceptRoutesFalse(msg string) bool {
	return regexp.MustCompile(`--accept-routes\s+is\s+false`).FindString(msg) != ""
}

// splitLines splits a string into lines (handles both \n and \r\n)
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace trims leading and trailing spaces from a string
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// hasActiveExitNode returns true if any peer or self is an active exit node.
func hasActiveExitNode(status *Status) bool {
	if status.Self.ExitNode {
		return true
	}
	for _, peer := range status.Peers {
		if peer.ExitNode {
			return true
		}
	}
	return false
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Printf("could not open link: %v", err)
	}
}

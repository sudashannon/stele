package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newShareManager keeps a test's tokens in its own data directory. Creating a
// share persists it, so without this a test run rewrites the real
// ~/.stele/share-tokens.json.
func newShareManager(t *testing.T, shareURL string) *ShareManager {
	t.Helper()
	t.Setenv("STELE_DATA_DIR", t.TempDir())
	return NewShareManager(shareURL)
}

func TestShareManager_CreateAndValidate(t *testing.T) {
	m := newShareManager(t, "")

	token, err := m.CreateShare("/x/design.md", "rx101", 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if token == "" {
		t.Fatal("got empty token")
	}

	entry, err := m.ValidateShare(token)
	if err != nil {
		t.Fatalf("ValidateShare: %v", err)
	}
	if entry.Path != "/x/design.md" {
		t.Errorf("path = %s, want /x/design.md", entry.Path)
	}
	if entry.Workspace != "rx101" {
		t.Errorf("workspace = %s, want rx101", entry.Workspace)
	}
}

func TestShareManager_ValidateReturnsErrorForUnknownToken(t *testing.T) {
	m := newShareManager(t, "")
	_, err := m.ValidateShare("nonexistent")
	if err == nil || err.Error() != "token not found" {
		t.Fatalf("expected 'token not found', got: %v", err)
	}
}

func TestShareManager_ValidateReturnsErrorForExpiredToken(t *testing.T) {
	m := newShareManager(t, "")
	token, err := m.CreateShare("/x/design.md", "", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err = m.ValidateShare(token)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected 'token expired', got: %v", err)
	}
}

func TestShareManager_Revoke(t *testing.T) {
	m := newShareManager(t, "")
	token, err := m.CreateShare("/x/design.md", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	if err := m.RevokeShare(token); err != nil {
		t.Fatalf("RevokeShare: %v", err)
	}

	_, err = m.ValidateShare(token)
	if err == nil || err.Error() != "token not found" {
		t.Fatalf("expected 'token not found' after revoke, got: %v", err)
	}
}

func TestShareManager_RevokeUnknownTokenReturnsError(t *testing.T) {
	m := newShareManager(t, "")
	err := m.RevokeShare("nonexistent")
	if err == nil || err.Error() != "token not found" {
		t.Fatalf("expected 'token not found', got: %v", err)
	}
}

// The host a caller reached the panel on is reachable from that caller by
// construction, so it is the origin their link must carry - not an address
// detected once at startup, which rots as soon as DHCP moves the adapter.
func TestShareURLUsesTheRequestHost(t *testing.T) {
	m := newShareManager(t, "")
	m.detect = func() string { t.Fatal("LAN detection must not run for a routable host"); return "" }

	req := httptest.NewRequest("POST", "http://192.168.1.50:8989/api/share/create", nil)
	if got := m.ShareURL(req, "tok"); got != "http://192.168.1.50:8989/share/tok" {
		t.Fatalf("ShareURL = %s, want http://192.168.1.50:8989/share/tok", got)
	}
}

// A DHCP move changes nothing about a request-derived origin: the same manager
// answers with whatever host the caller used next.
func TestShareURLFollowsTheHostAcrossRequests(t *testing.T) {
	m := newShareManager(t, "")

	first := m.ShareURL(httptest.NewRequest("GET", "http://10.0.8.247:8989/api/share/list", nil), "tok")
	second := m.ShareURL(httptest.NewRequest("GET", "http://10.0.28.45:8989/api/share/list", nil), "tok")

	if first != "http://10.0.8.247:8989/share/tok" {
		t.Fatalf("first = %s", first)
	}
	if second != "http://10.0.28.45:8989/share/tok" {
		t.Fatalf("second = %s, want the new host", second)
	}
}

// Loopback is the one host nobody else can use, so it is swapped for a detected
// LAN address while keeping the port the caller dialed (that is the port a
// portproxy forwards).
func TestShareURLSubstitutesLANForLoopbackKeepingPort(t *testing.T) {
	for _, host := range []string{"localhost:8989", "127.0.0.1:8989", "[::1]:8989", "0.0.0.0:8989"} {
		m := newShareManager(t, "")
		m.detect = func() string { return "10.0.28.45" }

		req := httptest.NewRequest("POST", "http://example.invalid/api/share/create", nil)
		req.Host = host
		if got := m.ShareURL(req, "tok"); got != "http://10.0.28.45:8989/share/tok" {
			t.Fatalf("host %s: ShareURL = %s, want http://10.0.28.45:8989/share/tok", host, got)
		}
	}
}

func TestShareURLKeepsPortlessLoopbackPortless(t *testing.T) {
	m := newShareManager(t, "")
	m.detect = func() string { return "10.0.28.45" }

	req := httptest.NewRequest("GET", "http://localhost/api/share/list", nil)
	if got := m.ShareURL(req, "tok"); got != "http://10.0.28.45/share/tok" {
		t.Fatalf("ShareURL = %s, want http://10.0.28.45/share/tok", got)
	}
}

// With no LAN address to offer, the caller keeps the host that demonstrably
// works for them rather than getting an empty or invented origin.
func TestShareURLFallsBackToLoopbackWhenNoLANFound(t *testing.T) {
	m := newShareManager(t, "")
	m.detect = func() string { return "" }

	req := httptest.NewRequest("GET", "http://localhost:8989/api/share/list", nil)
	if got := m.ShareURL(req, "tok"); got != "http://localhost:8989/share/tok" {
		t.Fatalf("ShareURL = %s, want http://localhost:8989/share/tok", got)
	}
}

func TestShareURLOverrideWinsOverTheRequestHost(t *testing.T) {
	m := newShareManager(t, "https://docs.example.com/")
	m.detect = func() string { t.Fatal("override must not consult detection"); return "" }

	req := httptest.NewRequest("GET", "http://10.0.28.45:8989/api/share/list", nil)
	if got := m.ShareURL(req, "tok"); got != "https://docs.example.com/share/tok" {
		t.Fatalf("ShareURL = %s, want https://docs.example.com/share/tok", got)
	}
}

func TestShareURLUsesHTTPSWhenForwardedAsTLS(t *testing.T) {
	m := newShareManager(t, "")

	req := httptest.NewRequest("GET", "http://panel.example.com/api/share/list", nil)
	req.Header.Set("X-Forwarded-Proto", "HTTPS")
	if got := m.ShareURL(req, "tok"); got != "https://panel.example.com/share/tok" {
		t.Fatalf("ShareURL = %s, want https://panel.example.com/share/tok", got)
	}
}

func TestListSharesUsesTheRequestHost(t *testing.T) {
	m := newShareManager(t, "")
	token, err := m.CreateShare("/x/design.md", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	shares := m.ListShares(httptest.NewRequest("GET", "http://192.168.1.50:8989/api/share/list", nil))
	if len(shares) != 1 {
		t.Fatalf("got %d shares, want 1", len(shares))
	}
	if want := "http://192.168.1.50:8989/share/" + token; shares[0].URL != want {
		t.Fatalf("URL = %s, want %s", shares[0].URL, want)
	}
}

// Detection spawns a process, so it is cached; the cache must still expire, or
// the staleness this replaced comes back with a longer fuse.
func TestLANDetectionIsCachedThenRefreshed(t *testing.T) {
	m := newShareManager(t, "")
	calls := 0
	ips := []string{"10.0.8.247", "10.0.28.45"}
	m.detect = func() string {
		ip := ips[min(calls, len(ips)-1)]
		calls++
		return ip
	}

	req := httptest.NewRequest("GET", "http://localhost:8989/api/share/list", nil)
	if got := m.ShareURL(req, "tok"); got != "http://10.0.8.247:8989/share/tok" {
		t.Fatalf("first = %s", got)
	}
	if got := m.ShareURL(req, "tok"); got != "http://10.0.8.247:8989/share/tok" {
		t.Fatalf("cached = %s", got)
	}
	if calls != 1 {
		t.Fatalf("detect ran %d times within the TTL, want 1", calls)
	}

	m.lanProbed = time.Now().Add(-lanTTL - time.Second)
	if got := m.ShareURL(req, "tok"); got != "http://10.0.28.45:8989/share/tok" {
		t.Fatalf("after TTL = %s, want the refreshed address", got)
	}
}

// Real ipconfig output from a Chinese Windows host: GBK bytes, localized field
// labels, a corporate VPN and the WSL bridge alongside the physical Wi-Fi.
const ipconfigFixture = "Windows IP \xc5\xe4\xd6\xc3\n" +
	"\n" +
	"\xce\xb4\xd6\xaa\xca\xca\xc5\xe4\xc6\xf7 CorpLink Wintun:\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 10.9.9.9\n" +
	"\n" +
	"\xce\xde\xcf\xdf\xbe\xd6\xd3\xf2\xcd\xf8\xca\xca\xc5\xe4\xc6\xf7 WLAN:\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 10.0.28.45\n" +
	"   \xd7\xd3\xcd\xf8\xd1\xda\xc2\xeb  . . . . . . . . . . : 255.255.240.0\n" +
	"\n" +
	"\xd2\xd4\xcc\xab\xcd\xf8\xca\xca\xc5\xe4\xc6\xf7 vEthernet (WSL (Hyper-V firewall)):\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 172.18.160.1\n"

func TestPickWindowsLANIPSkipsVirtualAndTunnelAdapters(t *testing.T) {
	if got := pickWindowsLANIP(ipconfigFixture); got != "10.0.28.45" {
		t.Fatalf("pickWindowsLANIP = %q, want 10.0.28.45 (the physical adapter)", got)
	}
}

func TestPickWindowsLANIPSkipsLinkLocalAndSubnetMasks(t *testing.T) {
	out := "Wireless LAN adapter Wi-Fi:\n" +
		"\n" +
		"   Autoconfiguration IPv4 Address. . : 169.254.21.145(Preferred)\n" +
		"   Subnet Mask . . . . . . . . . . . : 255.255.0.0\n" +
		"\n" +
		"Ethernet adapter Ethernet:\n" +
		"\n" +
		"   IPv4 Address. . . . . . . . . . . : 192.168.1.20(Preferred)\n"
	if got := pickWindowsLANIP(out); got != "192.168.1.20" {
		t.Fatalf("pickWindowsLANIP = %q, want 192.168.1.20", got)
	}
}

func TestPickWindowsLANIPReturnsEmptyWhenOnlyVirtualAdaptersExist(t *testing.T) {
	out := "Ethernet adapter vEthernet (WSL):\n" +
		"\n" +
		"   IPv4 Address. . . . . . . . . . . : 172.18.160.1\n" +
		"\n" +
		"Ethernet adapter VirtualBox Host-Only Network:\n" +
		"\n" +
		"   IPv4 Address. . . . . . . . . . . : 192.168.56.1\n"
	if got := pickWindowsLANIP(out); got != "" {
		t.Fatalf("pickWindowsLANIP = %q, want empty", got)
	}
}

// Real output from the host that produced the bug: a phone or dongle sharing its
// connection appears as an ordinary "以太网 4" adapter holding a Remote NDIS
// device, with DHCP, a gateway and a private /24. No adapter-name rule can tell
// it from the office Ethernet, and Windows even prefers its default route
// (metric 25 against the WLAN's 45), so the share link pointed at a network
// nobody else is on.
const tetheredIPConfigFixture = "Windows IP \xc5\xe4\xd6\xc3\n" +
	"\n" +
	"\xd2\xd4\xcc\xab\xcd\xf8\xca\xca\xc5\xe4\xc6\xf7 \xd2\xd4\xcc\xab\xcd\xf8 4:\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 192.168.123.2\n" +
	"   \xc4\xac\xc8\xcf\xcd\xf8\xb9\xd8. . . . . . . . . . : 192.168.123.1\n" +
	"\n" +
	"\xce\xde\xcf\xdf\xbe\xd6\xd3\xf2\xcd\xf8\xca\xca\xc5\xe4\xc6\xf7 WLAN:\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 10.0.28.45\n" +
	"   \xc4\xac\xc8\xcf\xcd\xf8\xb9\xd8. . . . . . . . . . : 10.0.24.1\n" +
	"\n" +
	"\xd2\xd4\xcc\xab\xcd\xf8\xca\xca\xc5\xe4\xc6\xf7 vEthernet (WSL (Hyper-V firewall)):\n" +
	"\n" +
	"   IPv4 \xb5\xd8\xd6\xb7 . . . . . . . . . . . . : 172.18.160.1\n"

// The matching neighbour table. The tether shows only its own gateway; the
// office subnet shows other machines. Entry types are localized GBK, which is
// why the parser must not read them.
const tetheredARPFixture = "\n\xbd\xd3\xbf\xda: 192.168.123.2 --- 0xd\n" +
	"  Internet \xb5\xd8\xd6\xb7         \xceïżœ\xed\xb5\xd8\xd6\xb7           \xc0\xe0\xd0\xcd\n" +
	"  192.168.123.1         12-e9-ef-4a-1e-01     \xb6\xaf\xcc\xac\n" +
	"  192.168.123.255       ff-ff-ff-ff-ff-ff     \xbe\xb2\xcc\xac\n" +
	"  224.0.0.22            01-00-5e-00-00-16     \xbe\xb2\xcc\xac\n" +
	"  239.255.255.250       01-00-5e-7f-ff-fa     \xbe\xb2\xcc\xac\n" +
	"\n\xbd\xd3\xbf\xda: 10.0.28.45 --- 0x1e\n" +
	"  10.0.24.1             00-11-22-33-44-55     \xb6\xaf\xcc\xac\n" +
	"  10.0.24.135           00-11-22-33-44-56     \xb6\xaf\xcc\xac\n" +
	"  10.0.25.27            00-11-22-33-44-57     \xb6\xaf\xcc\xac\n" +
	"  10.0.30.199           00-11-22-33-44-58     \xb6\xaf\xcc\xac\n" +
	"  10.0.31.255           ff-ff-ff-ff-ff-ff     \xbe\xb2\xcc\xac\n" +
	"  224.0.0.22            01-00-5e-00-00-16     \xbe\xb2\xcc\xac\n"

func TestLANPickerPrefersTheSubnetWithOtherHosts(t *testing.T) {
	candidates := windowsLANCandidates(tetheredIPConfigFixture)
	if len(candidates) != 2 || candidates[0] != "192.168.123.2" {
		t.Fatalf("candidates = %v, want the tether first (declaration order) and the WLAN second", candidates)
	}
	if got := preferLANWithPeers(candidates, tetheredARPFixture); got != "10.0.28.45" {
		t.Fatalf("preferLANWithPeers = %q, want 10.0.28.45: the tethered /24 holds only its gateway", got)
	}
}

func TestLANPickerKeepsDeclarationOrderWhenEveryCandidateOnlyHasItsGateway(t *testing.T) {
	candidates := windowsLANCandidates(tetheredIPConfigFixture)
	// A quiet network: both adapters are up and each has resolved only its own
	// router, so there is no evidence either way and the declared order stands.
	quiet := "Interface: 192.168.123.2 --- 0xd\n" +
		"  192.168.123.1         12-e9-ef-4a-1e-01     dynamic\n" +
		"Interface: 10.0.28.45 --- 0x1e\n" +
		"  10.0.24.1             00-11-22-33-44-55     dynamic\n"
	if got := preferLANWithPeers(candidates, quiet); got != "192.168.123.2" {
		t.Fatalf("preferLANWithPeers = %q, want the first candidate on a tie", got)
	}
	if got := preferLANWithPeers(candidates, ""); got != "192.168.123.2" {
		t.Fatalf("preferLANWithPeers = %q, want the first candidate when arp says nothing", got)
	}
}

func TestLANPickerPrefersAnActiveAdapterOverASilentOne(t *testing.T) {
	candidates := windowsLANCandidates(tetheredIPConfigFixture)
	// The tether is up but has resolved nobody; the WLAN has reached its router.
	// One neighbour is thin evidence, but it beats none.
	out := "Interface: 192.168.123.2 --- 0xd\n" +
		"Interface: 10.0.28.45 --- 0x1e\n" +
		"  10.0.24.1             00-11-22-33-44-55     dynamic\n"
	if got := preferLANWithPeers(candidates, out); got != "10.0.28.45" {
		t.Fatalf("preferLANWithPeers = %q, want the adapter that has resolved somebody", got)
	}
}

func TestARPPeerCountsExcludesBroadcastMulticastAndSelf(t *testing.T) {
	counts := arpPeerCounts(tetheredARPFixture)
	if counts["192.168.123.2"] != 1 {
		t.Fatalf("tether peers = %d, want 1 (its gateway only)", counts["192.168.123.2"])
	}
	if counts["10.0.28.45"] != 4 {
		t.Fatalf("office peers = %d, want 4 (gateway plus three hosts, no broadcast or multicast)", counts["10.0.28.45"])
	}
}

func TestARPPeerCountsIgnoresAnInterfacesOwnAddress(t *testing.T) {
	out := "Interface: 10.0.28.45 --- 0x1e\n" +
		"  10.0.28.45            04-f0-ee-90-27-52     dynamic\n" +
		"  10.0.24.1             00-11-22-33-44-55     dynamic\n"
	if got := arpPeerCounts(out)["10.0.28.45"]; got != 1 {
		t.Fatalf("peers = %d, want 1: an interface is not its own neighbour", got)
	}
}

func TestShareManager_SweepCleansExpiredTokens(t *testing.T) {
	m := newShareManager(t, "")
	token1, _ := m.CreateShare("/x/1.md", "", 1*time.Millisecond)
	_, _ = m.CreateShare("/x/2.md", "", 1*time.Hour)
	time.Sleep(10 * time.Millisecond)

	m.sweep()

	if _, err := m.ValidateShare(token1); err == nil {
		t.Fatal("expected token1 to be swept out")
	}
}

// The public listener is a security boundary: it must carry the share pages and
// nothing else. Registering a route here rather than refusing it in a check is
// the point - there is no handler on the public port for /api/* to reach.
func TestPublicShareMuxServesOnlySharePages(t *testing.T) {
	m := newShareManager(t, "")
	dir := t.TempDir()
	doc := filepath.Join(dir, "design.md")
	if err := os.WriteFile(doc, []byte("# Design\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := m.CreateShare(doc, "rx101", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mux := publicShareMux(m)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "http://10.0.28.45:8989/share/"+token, nil))
	if rec.Code != 200 {
		t.Fatalf("share page = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Design") {
		t.Fatalf("share page did not render the document: %q", rec.Body.String()[:min(120, rec.Body.Len())])
	}

	// Everything the panel serves must be absent from this mux. /api/artifact
	// would hand over any authorized file; /mcp accepts tool calls; / is the SPA.
	for _, path := range []string{
		"/api/artifact?path=" + doc,
		"/api/wiki/graph",
		"/api/workspaces",
		"/api/share/list",
		"/api/todos",
		"/mcp",
		"/",
		"/assets/index.js",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "http://10.0.28.45:8989"+path, nil))
		if rec.Code != 404 {
			t.Errorf("%s = %d on the public mux, want 404: it must not be published", path, rec.Code)
		}
	}
}

// With the panel on a private port, a link built for a loopback caller must carry
// the public share port. Keeping the dialed port would publish an address that is
// deliberately unreachable from the network.
func TestShareURLUsesThePublicPortWhenOneIsPublished(t *testing.T) {
	m := newShareManager(t, "")
	m.detect = func() string { return "10.0.28.45" }
	m.SetPublicPort(8989)

	req := httptest.NewRequest("GET", "http://localhost:18989/api/share/create", nil)
	if got := m.ShareURL(req, "tok"); got != "http://10.0.28.45:8989/share/tok" {
		t.Fatalf("ShareURL = %s, want the public share port 8989, not the panel's 18989", got)
	}
}

// Without a share listener the dialed port is still the right answer: that is the
// port a portproxy forwards.
func TestShareURLKeepsTheDialedPortWithoutAPublicListener(t *testing.T) {
	m := newShareManager(t, "")
	m.detect = func() string { return "10.0.28.45" }

	req := httptest.NewRequest("GET", "http://localhost:8989/api/share/create", nil)
	if got := m.ShareURL(req, "tok"); got != "http://10.0.28.45:8989/share/tok" {
		t.Fatalf("ShareURL = %s, want the dialed port preserved", got)
	}
}

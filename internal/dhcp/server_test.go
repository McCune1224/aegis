package dhcp_test

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/client"
	"aegis/internal/dhcp"
	"aegis/internal/filter"
	"aegis/internal/store"
)

func hardware(t *testing.T, raw string) net.HardwareAddr {
	t.Helper()
	parsed, err := net.ParseMAC(raw)
	require.NoError(t, err)
	return parsed
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "aegis.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// packet builds a client packet the way a client does: op 1, ethernet, the
// hardware address, the broadcast flag, and the options a caller adds.
func packet(kind dhcp.Type, xid uint32, mac net.HardwareAddr, extra ...[]byte) []byte {
	body := make([]byte, 240)
	body[0] = 1
	body[1] = 1
	body[2] = 6
	binary.BigEndian.PutUint32(body[4:8], xid)
	body[10] = 0x80
	copy(body[28:34], mac)
	copy(body[236:240], []byte{99, 130, 83, 99})
	body = append(body, 53, 1, byte(kind))
	for _, option := range extra {
		body = append(body, option...)
	}
	return append(body, 255)
}

// requested adds the "I would like this address" option.
func requested(address netip.Addr) []byte {
	return append([]byte{50, 4}, address.AsSlice()...)
}

// accepted adds the server identifier a client names in a request.
func accepted(address netip.Addr) []byte {
	return append([]byte{54, 4}, address.AsSlice()...)
}

func hostname(name string) []byte {
	return append([]byte{12, byte(len(name))}, []byte(name)...)
}

// harness is one running server on an ephemeral port, a client socket next to
// it, and the pieces a test asserts on afterwards.
type harness struct {
	server     *dhcp.Server
	database   *store.Store
	leases     *client.Dynamic
	identity   atomic.Pointer[client.Resolver]
	client     *net.UDPConn
	serverAddr netip.AddrPort
}

// claimMAC is the hardware address the nth claimed device has, so a test can
// name the clients it wants and still know their addresses.
func claimMAC(index int) net.HardwareAddr {
	return net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, byte(index + 1)}
}

// claim makes each key a client that owns one hardware address, in the store
// and in the identity resolver the server asks.
func (h *harness) claim(t *testing.T, keys ...string) {
	t.Helper()
	specs := make([]client.Spec, 0, len(keys))
	for index, key := range keys {
		mac := claimMAC(index)
		require.NoError(t, h.database.SaveClient(t.Context(), store.Client{
			Key: filter.ClientKey(key), Profile: "default", MACs: []net.HardwareAddr{mac},
		}))
		specs = append(specs, client.Spec{Key: filter.ClientKey(key), MACs: []net.HardwareAddr{mac}})
	}

	resolver, err := client.New(specs)
	require.NoError(t, err)
	resolver.UseLeases(h.leases)
	h.identity.Store(resolver)
}

// key resolves an address the way the DNS path does.
func (h *harness) key(address netip.Addr) filter.ClientKey {
	return h.identity.Load().Key(address)
}

func (h *harness) exchange(t *testing.T, packet []byte) dhcp.Message {
	t.Helper()
	_, err := h.client.WriteToUDP(packet, net.UDPAddrFromAddrPort(h.serverAddr))
	require.NoError(t, err)
	return h.read(t)
}

func (h *harness) read(t *testing.T) dhcp.Message {
	t.Helper()
	require.NoError(t, h.client.SetReadDeadline(time.Now().Add(2*time.Second)))
	buffer := make([]byte, 1500)
	read, _, err := h.client.ReadFromUDP(buffer)
	require.NoError(t, err)
	message, err := dhcp.Parse(buffer[:read])
	require.NoError(t, err)
	return message
}

// start runs a server over a one-address pool unless the caller says otherwise,
// with the store, the lease table, and the identity resolver the server uses.
func start(t *testing.T, configure func(*dhcp.Config), claims ...string) *harness {
	t.Helper()
	database := openStore(t)
	leases := client.NewDynamic()

	h := &harness{database: database, leases: leases}
	h.claim(t, claims...)

	config := dhcp.Config{
		Address:   "127.0.0.1:0",
		Pool:      dhcp.Pool{Network: netip.MustParsePrefix("10.9.9.0/24"), First: netip.MustParseAddr("10.9.9.100"), Last: netip.MustParseAddr("10.9.9.100")},
		ServerIP:  netip.MustParseAddr("10.9.9.1"),
		Router:    netip.MustParseAddr("10.9.9.1"),
		DNS:       []netip.Addr{netip.MustParseAddr("10.9.9.1")},
		LeaseTime: time.Hour,
		Identity: func(selector client.Selector) filter.ClientKey {
			return h.identity.Load().Select(selector)
		},
	}
	if configure != nil {
		configure(&config)
	}

	server := dhcp.New(config, database, leases, nil)
	require.NoError(t, server.Load(t.Context()))
	_, err := server.Sweep(t.Context())
	require.NoError(t, err)

	var listen net.ListenConfig
	conn, err := listen.ListenPacket(t.Context(), "udp4", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- server.ServeConn(ctx, conn) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-served)
	})

	clientConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })

	serverAddr, err := netip.ParseAddrPort(conn.LocalAddr().String())
	require.NoError(t, err)

	h.server = server
	h.client = clientConn
	h.serverAddr = serverAddr
	return h
}

func TestAServerOffersThenAcksAndRecordsTheLease(t *testing.T) {
	h := start(t, nil, "tablet")
	mac := hardware(t, "aa:bb:cc:dd:ee:01")

	offer := h.exchange(t, packet(dhcp.Discover, 0x1234, mac))
	require.Equal(t, dhcp.Offer, offer.Type)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), offer.YourIP)
	require.Equal(t, netip.MustParseAddr("10.9.9.1"), offer.ServerID)
	require.Equal(t, []byte{255, 255, 255, 0}, offer.Options[dhcp.OptionSubnetMask])
	require.Equal(t, []byte{10, 9, 9, 1}, offer.Options[dhcp.OptionDNS])
	require.Equal(t, []byte{0x00, 0x00, 0x0e, 0x10}, offer.Options[dhcp.OptionLeaseTime], "an hour")

	ack := h.exchange(t, packet(dhcp.Request, 0x1234, mac,
		requested(netip.MustParseAddr("10.9.9.100")), accepted(netip.MustParseAddr("10.9.9.1"))))
	require.Equal(t, dhcp.Ack, ack.Type)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), ack.YourIP)

	leases, err := h.database.Leases(t.Context())
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), leases[0].Address)
	require.Equal(t, mac, leases[0].MAC)
	require.Equal(t, filter.ClientKey("tablet"), leases[0].Client)

	held, ok := h.leases.MAC(netip.MustParseAddr("10.9.9.100"))
	require.True(t, ok)
	require.Equal(t, mac, held)
}

func TestADeviceNoClientClaimsBecomesADiscovery(t *testing.T) {
	h := start(t, nil)
	mac := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x99}

	h.exchange(t, packet(dhcp.Discover, 7, mac, hostname("phone"), requested(netip.MustParseAddr("10.9.9.100"))))

	discoveries, err := h.database.Discoveries(t.Context())
	require.NoError(t, err)
	require.Len(t, discoveries, 1)
	require.Equal(t, mac, discoveries[0].MAC)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), discoveries[0].Address)
	require.Equal(t, "phone", discoveries[0].Hostname)
}

func TestAClaimedDeviceIsNotADiscoveryAndItsLeaseCarriesItsIdentity(t *testing.T) {
	h := start(t, nil, "tablet")
	mac := hardware(t, "aa:bb:cc:dd:ee:01")

	ack := h.exchange(t, packet(dhcp.Request, 9, mac, requested(netip.MustParseAddr("10.9.9.100"))))
	require.Equal(t, dhcp.Ack, ack.Type)

	discoveries, err := h.database.Discoveries(t.Context())
	require.NoError(t, err)
	require.Empty(t, discoveries, "a claimed device is not a prompt")

	require.Equal(t, filter.ClientKey("tablet"), h.key(netip.MustParseAddr("10.9.9.100")),
		"the address the lease handed out resolves to the device's policy")
}

func TestClaimingADeviceClearsItsDiscovery(t *testing.T) {
	h := start(t, nil)
	// The address the operator is about to make a client of.
	mac := claimMAC(0)
	h.exchange(t, packet(dhcp.Discover, 11, mac, requested(netip.MustParseAddr("10.9.9.100"))))

	discoveries, err := h.database.Discoveries(t.Context())
	require.NoError(t, err)
	require.Len(t, discoveries, 1)

	// The operator makes a client of the device, and the server hears from it
	// again.
	h.claim(t, "phone")

	h.exchange(t, packet(dhcp.Request, 12, mac, requested(netip.MustParseAddr("10.9.9.100")), accepted(netip.MustParseAddr("10.9.9.1"))))

	discoveries, err = h.database.Discoveries(t.Context())
	require.NoError(t, err)
	require.Empty(t, discoveries)
}

func TestARequestForAnAddressThePoolCannotGiveIsRefused(t *testing.T) {
	h := start(t, nil)
	mac := hardware(t, "aa:bb:cc:dd:ee:01")

	nak := h.exchange(t, packet(dhcp.Request, 13, mac, requested(netip.MustParseAddr("10.9.9.240")), accepted(netip.MustParseAddr("10.9.9.1"))))
	require.Equal(t, dhcp.Nak, nak.Type)
	require.False(t, nak.YourIP.IsValid())

	leases, err := h.database.Leases(t.Context())
	require.NoError(t, err)
	require.Empty(t, leases)
}

func TestARequestForAnotherDevicesAddressIsRefused(t *testing.T) {
	h := start(t, nil)
	h.exchange(t, packet(dhcp.Discover, 14, hardware(t, "aa:bb:cc:dd:ee:01")))

	nak := h.exchange(t, packet(dhcp.Request, 15, hardware(t, "aa:bb:cc:dd:ee:02"), requested(netip.MustParseAddr("10.9.9.100"))))
	require.Equal(t, dhcp.Nak, nak.Type)

	leases, err := h.database.Leases(t.Context())
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, hardware(t, "aa:bb:cc:dd:ee:01"), leases[0].MAC, "the first device keeps its address")
}

func TestAReleaseGivesTheAddressBack(t *testing.T) {
	h := start(t, nil, "tablet")
	mac := hardware(t, "aa:bb:cc:dd:ee:01")
	h.exchange(t, packet(dhcp.Request, 16, mac, requested(netip.MustParseAddr("10.9.9.100"))))

	release := packet(dhcp.Release, 17, mac)
	copy(release[12:16], []byte{10, 9, 9, 100})
	_, err := h.client.WriteToUDP(release, net.UDPAddrFromAddrPort(h.serverAddr))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		leases, err := h.database.Leases(t.Context())
		return err == nil && len(leases) == 0
	}, 2*time.Second, 10*time.Millisecond, "the lease outlived the release")

	_, still := h.leases.MAC(netip.MustParseAddr("10.9.9.100"))
	require.False(t, still)
}

func TestAnExpiredLeaseGoesBackIntoThePool(t *testing.T) {
	// The lease is long enough that the two exchanges cannot outlive it, and the
	// clock the server reads is one the test advances, so nothing here depends on
	// how fast the machine is.
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	h := start(t, func(config *dhcp.Config) {
		config.LeaseTime = time.Hour
		config.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	})
	first := hardware(t, "aa:bb:cc:dd:ee:01")
	second := hardware(t, "aa:bb:cc:dd:ee:02")

	h.exchange(t, packet(dhcp.Discover, 18, first))
	h.expectNoAnswer(t, packet(dhcp.Discover, 99, second))

	clock.Store(time.Now().Add(2 * time.Hour).UnixNano())
	removed, err := h.server.Sweep(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	offer := h.exchange(t, packet(dhcp.Discover, 19, second))
	require.Equal(t, dhcp.Offer, offer.Type)
	require.Equal(t, netip.MustParseAddr("10.9.9.100"), offer.YourIP, "the expired lease is offered again")
}

// expectNoAnswer sends a packet and requires that nothing comes back, which is
// what an exhausted pool looks like.
func (h *harness) expectNoAnswer(t *testing.T, packet []byte) {
	t.Helper()
	_, err := h.client.WriteToUDP(packet, net.UDPAddrFromAddrPort(h.serverAddr))
	require.NoError(t, err)
	require.NoError(t, h.client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buffer := make([]byte, 1500)
	_, _, err = h.client.ReadFromUDP(buffer)
	require.Error(t, err, "an exhausted pool answers nothing")
}

func TestParseRangeReadsARangeAndItsNetmask(t *testing.T) {
	pool, err := dhcp.ParseRange("10.9.9.100-10.9.9.200", "24")
	require.NoError(t, err)
	require.Equal(t, netip.MustParsePrefix("10.9.9.0/24"), pool.Network)
	require.True(t, pool.Contains(netip.MustParseAddr("10.9.9.100")))
	require.True(t, pool.Contains(netip.MustParseAddr("10.9.9.200")))
	require.False(t, pool.Contains(netip.MustParseAddr("10.9.9.201")))

	dotted, err := dhcp.ParseRange("10.9.9.100 - 10.9.9.200", "255.255.255.0")
	require.NoError(t, err)
	require.Equal(t, pool, dotted)

	_, err = dhcp.ParseRange("10.9.9.200-10.9.9.100", "24")
	require.ErrorContains(t, err, "ends before it starts")

	_, err = dhcp.ParseRange("10.9.9.100-10.9.10.10", "24")
	require.ErrorContains(t, err, "not inside")

	_, err = dhcp.ParseRange("10.9.9.100", "24")
	require.ErrorContains(t, err, "not start-end")

	_, err = dhcp.ParseRange("10.9.9.100-10.9.9.200", "")
	require.ErrorContains(t, err, "netmask")
}

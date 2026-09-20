package dhcp_test

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/dhcp"
)

// discover is a DHCPDISCOVER as a client puts it on the wire: op 1, ethernet,
// xid 0x12345678, the broadcast flag, chaddr aa:bb:cc:dd:ee:01, and options for
// the message type, the address it would like, and its hostname.
func discover() []byte {
	packet := make([]byte, 240, 258)
	packet[0] = 1 // op: request
	packet[1] = 1 // htype: ethernet
	packet[2] = 6 // hlen
	packet[3] = 0 // hops
	copy(packet[4:8], []byte{0x12, 0x34, 0x56, 0x78})
	packet[10] = 0x80 // the broadcast flag
	copy(packet[28:34], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01})
	copy(packet[236:240], []byte{99, 130, 83, 99})
	return append(packet, []byte{
		53, 1, 1, // message type: DISCOVER
		50, 4, 10, 9, 9, 20, // requested address
		12, 6, 't', 'a', 'b', 'l', 'e', 't', // hostname
		255,
	}...)
}

func TestParseReadsTheDiscoverWire(t *testing.T) {
	message, err := dhcp.Parse(discover())
	require.NoError(t, err)

	require.Equal(t, dhcp.OpRequest, message.Op)
	require.Equal(t, uint32(0x12345678), message.XID)
	require.Equal(t, dhcp.Discover, message.Type)
	require.True(t, message.Broadcast)
	require.Equal(t, net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}, message.MAC)
	require.Equal(t, netip.MustParseAddr("10.9.9.20"), message.RequestedIP)
	require.Equal(t, "tablet", message.Hostname)
}

func TestParseRefusesAPacketThatIsNotDHCP(t *testing.T) {
	short := discover()[:100]
	_, err := dhcp.Parse(short)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")

	wrongCookie := discover()
	copy(wrongCookie[236:240], []byte{1, 2, 3, 4})
	_, err = dhcp.Parse(wrongCookie)
	require.Error(t, err)
	require.Contains(t, err.Error(), "magic cookie")
}

func TestParseStopsAtTheEndOption(t *testing.T) {
	// A trailing option after the end marker is not part of the message, which
	// is what keeps a padded packet from being read as options.
	packet := append(discover(), 12, 4, 'b', 'a', 'd', 255, 12, 2, 'x', 'y', 255)
	message, err := dhcp.Parse(packet)
	require.NoError(t, err)
	require.Equal(t, "tablet", message.Hostname)
}

func TestReplyMarshalsWhatAClientNeedsToConfigureItself(t *testing.T) {
	reply := dhcp.Reply{
		Type:       dhcp.Offer,
		XID:        0x12345678,
		Broadcast:  true,
		MAC:        net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01},
		YourIP:     netip.MustParseAddr("10.9.9.20"),
		ServerIP:   netip.MustParseAddr("10.9.9.1"),
		SubnetMask: netip.MustParsePrefix("10.9.9.0/24"),
		Router:     netip.MustParseAddr("10.9.9.1"),
		DNS:        []netip.Addr{netip.MustParseAddr("10.9.9.1")},
		LeaseTime:  12 * time.Hour,
		Hostname:   "tablet",
	}

	packet := reply.Marshal()
	require.GreaterOrEqual(t, len(packet), 240)
	require.Equal(t, byte(2), packet[0], "a reply carries op 2")
	require.Equal(t, byte(1), packet[1], "ethernet")
	require.Equal(t, byte(6), packet[2], "six bytes of hardware address")
	require.Equal(t, []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}, packet[28:34])
	require.Equal(t, []byte{0x12, 0x34, 0x56, 0x78}, packet[4:8])
	require.Equal(t, byte(0x80), packet[10], "the broadcast flag comes back")
	require.Equal(t, []byte{10, 9, 9, 20}, packet[16:20], "yiaddr is the address being offered")
	require.Equal(t, []byte{10, 9, 9, 1}, packet[20:24], "siaddr is the server")
	require.Equal(t, []byte{99, 130, 83, 99}, packet[236:240])

	decoded, err := dhcp.Parse(packet)
	require.NoError(t, err)
	require.Equal(t, []byte{2}, decoded.Options[dhcp.OptionMessageType], "an offer is type 2")
	require.Equal(t, []byte{10, 9, 9, 1}, decoded.Options[dhcp.OptionServerID])
	require.Equal(t, []byte{255, 255, 255, 0}, decoded.Options[dhcp.OptionSubnetMask])
	require.Equal(t, []byte{10, 9, 9, 1}, decoded.Options[dhcp.OptionRouter])
	require.Equal(t, []byte{10, 9, 9, 1}, decoded.Options[dhcp.OptionDNS])
	require.Equal(t, []byte{0x00, 0x00, 0xa8, 0xc0}, decoded.Options[dhcp.OptionLeaseTime], "the lease is 12 hours")
	require.Equal(t, []byte("tablet"), decoded.Options[dhcp.OptionHostname])
}

func TestAReplyRoundTripsThroughTheParser(t *testing.T) {
	reply := dhcp.Reply{
		Type:       dhcp.Ack,
		XID:        7,
		MAC:        net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02},
		YourIP:     netip.MustParseAddr("10.9.9.21"),
		ServerIP:   netip.MustParseAddr("10.9.9.1"),
		SubnetMask: netip.MustParsePrefix("10.9.9.0/24"),
		LeaseTime:  time.Hour,
	}

	message, err := dhcp.Parse(reply.Marshal())
	require.NoError(t, err)
	require.Equal(t, dhcp.OpReply, message.Op)
	require.Equal(t, dhcp.Ack, message.Type)
	require.Equal(t, uint32(7), message.XID)
	require.Equal(t, net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02}, message.MAC)
	require.Equal(t, netip.MustParseAddr("10.9.9.21"), message.YourIP)
	require.Equal(t, netip.MustParseAddr("10.9.9.1"), message.ServerID)
}

func TestParseReadsAReleaseThatHasNoRequestedAddress(t *testing.T) {
	packet := make([]byte, 240, 244)
	packet[0] = 1
	packet[1] = 1
	packet[2] = 6
	copy(packet[12:16], []byte{10, 9, 9, 20}) // ciaddr: the address being released
	copy(packet[28:34], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01})
	copy(packet[236:240], []byte{99, 130, 83, 99})
	packet = append(packet, 53, 1, 7, 255)

	message, err := dhcp.Parse(packet)
	require.NoError(t, err)
	require.Equal(t, dhcp.Release, message.Type)
	require.Equal(t, netip.MustParseAddr("10.9.9.20"), message.ClientIP)
	require.False(t, message.RequestedIP.IsValid())
}

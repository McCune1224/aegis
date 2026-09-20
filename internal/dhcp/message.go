// Package dhcp serves DHCPv4: it parses the wire, hands out addresses from a
// configured pool, keeps the leases in the store, and reports the devices no
// client record claims. Every packet is untyped until Parse or Marshal turns it
// into the typed shapes below.
package dhcp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Op is the operation field: a request from a client, or a reply from a server.
type Op uint8

const (
	OpRequest Op = 1
	OpReply   Op = 2
)

// Type is the message type option (53).
type Type uint8

const (
	Discover Type = 1
	Offer    Type = 2
	Request  Type = 3
	Decline  Type = 4
	Ack      Type = 5
	Nak      Type = 6
	Release  Type = 7
	Inform   Type = 8
)

func (t Type) String() string {
	switch t {
	case Discover:
		return "discover"
	case Offer:
		return "offer"
	case Request:
		return "request"
	case Decline:
		return "decline"
	case Ack:
		return "ack"
	case Nak:
		return "nak"
	case Release:
		return "release"
	case Inform:
		return "inform"
	default:
		return fmt.Sprintf("type %d", uint8(t))
	}
}

// Option codes this package reads or writes.
const (
	OptionSubnetMask  byte = 1
	OptionRouter      byte = 3
	OptionDNS         byte = 6
	OptionHostname    byte = 12
	OptionRequestedIP byte = 50
	OptionLeaseTime   byte = 51
	OptionMessageType byte = 53
	OptionServerID    byte = 54
	OptionParameter   byte = 55
	OptionRenewal     byte = 58
	OptionRebinding   byte = 59
	OptionEnd         byte = 255
)

// headerLen is the fixed part of a DHCPv4 packet, the magic cookie included.
const headerLen = 240

// cookie is the RFC 2131 magic cookie that marks a DHCP packet as one.
var cookie = [4]byte{99, 130, 83, 99}

// Message is one decoded packet. The fields a caller branches on are typed;
// the rest of the options stay as the bytes they arrived as.
type Message struct {
	Op          Op
	XID         uint32
	Broadcast   bool
	MAC         net.HardwareAddr
	ClientIP    netip.Addr // ciaddr: the address a client already holds
	YourIP      netip.Addr // yiaddr: the address the server is handing over
	ServerIP    netip.Addr // siaddr
	GatewayIP   netip.Addr // giaddr: the relay the request came through
	Type        Type
	RequestedIP netip.Addr // option 50
	ServerID    netip.Addr // option 54
	Hostname    string     // option 12
	Options     map[byte][]byte
}

// Parse decodes one DHCPv4 packet. Everything past the fixed header is options,
// and an option that runs past the end of the packet is an error rather than a
// silent truncation, because a short read here would answer the wrong client.
func Parse(packet []byte) (Message, error) {
	if len(packet) < headerLen {
		return Message{}, fmt.Errorf("dhcp: packet is too short: %d bytes", len(packet))
	}
	if [4]byte(packet[236:240]) != cookie {
		return Message{}, fmt.Errorf("dhcp: packet has no magic cookie")
	}

	message := Message{
		Op:        Op(packet[0]),
		XID:       binary.BigEndian.Uint32(packet[4:8]),
		Broadcast: packet[10]&0x80 != 0,
		Options:   make(map[byte][]byte),
	}
	hardwareLen := int(packet[2])
	if hardwareLen > 16 {
		return Message{}, fmt.Errorf("dhcp: hardware address length %d is out of range", hardwareLen)
	}
	message.MAC = net.HardwareAddr(packet[28 : 28+hardwareLen])

	for field, raw := range map[*netip.Addr][]byte{
		&message.ClientIP:  packet[12:16],
		&message.YourIP:    packet[16:20],
		&message.ServerIP:  packet[20:24],
		&message.GatewayIP: packet[24:28],
	} {
		*field = optionalAddress(raw)
	}

	if err := parseOptions(packet[headerLen:], &message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func parseOptions(options []byte, message *Message) error {
	for offset := 0; offset < len(options); {
		code := options[offset]
		if code == 0 { // padding
			offset++
			continue
		}
		if code == OptionEnd {
			return nil
		}
		if offset+1 >= len(options) {
			return fmt.Errorf("dhcp: option %d runs past the packet", code)
		}
		length := int(options[offset+1])
		start := offset + 2
		if start+length > len(options) {
			return fmt.Errorf("dhcp: option %d says %d bytes, past the packet", code, length)
		}
		value := options[start : start+length]
		message.Options[code] = value

		switch code {
		case OptionMessageType:
			if len(value) == 1 {
				message.Type = Type(value[0])
			}
		case OptionRequestedIP:
			message.RequestedIP = addressFrom(value)
		case OptionServerID:
			message.ServerID = addressFrom(value)
		case OptionHostname:
			message.Hostname = string(value)
		}
		offset = start + length
	}
	return nil
}

// optionalAddress reads one of the header's address fields. An all-zero field
// is the wire's way of saying the field is not set, so it becomes the zero
// address rather than the unspecified one.
func optionalAddress(value []byte) netip.Addr {
	address := addressFrom(value)
	if address.IsUnspecified() {
		return netip.Addr{}
	}
	return address
}

func addressFrom(value []byte) netip.Addr {
	address, ok := netip.AddrFromSlice(value)
	if !ok {
		return netip.Addr{}
	}
	return address
}

// Reply is what a server sends one client. It carries the addresses and options
// a client needs to configure its interface, and nothing else.
type Reply struct {
	Type       Type
	XID        uint32
	Broadcast  bool
	MAC        net.HardwareAddr
	YourIP     netip.Addr
	ServerIP   netip.Addr
	SubnetMask netip.Prefix
	Router     netip.Addr
	DNS        []netip.Addr
	LeaseTime  time.Duration
	Hostname   string
}

// Marshal encodes the reply. The option order is the one the field order
// suggests: the message type and server first, then the addressing the client
// applies, then the lease timers.
func (r Reply) Marshal() []byte {
	packet := make([]byte, headerLen)
	packet[0] = byte(OpReply)
	packet[1] = 1 // ethernet
	packet[2] = 6
	binary.BigEndian.PutUint32(packet[4:8], r.XID)
	if r.Broadcast {
		packet[10] = 0x80
	}
	copy(packet[16:20], r.YourIP.AsSlice())
	copy(packet[20:24], r.ServerIP.AsSlice())
	copy(packet[28:34], r.MAC)
	copy(packet[236:240], cookie[:])

	options := []option{
		{OptionMessageType, []byte{byte(r.Type)}},
		{OptionServerID, r.ServerIP.AsSlice()},
	}
	if r.LeaseTime > 0 {
		seconds := uint32(r.LeaseTime / time.Second)
		options = append(options,
			option{OptionLeaseTime, binary.BigEndian.AppendUint32(nil, seconds)},
			// The renewal and rebinding timers are the RFC 2131 defaults, half
			// the lease and seven eighths of it.
			option{OptionRenewal, binary.BigEndian.AppendUint32(nil, seconds/2)},
			option{OptionRebinding, binary.BigEndian.AppendUint32(nil, seconds*7/8)},
		)
	}
	if r.SubnetMask.IsValid() {
		options = append(options, option{OptionSubnetMask, net.CIDRMask(r.SubnetMask.Bits(), 32)})
	}
	if r.Router.IsValid() {
		options = append(options, option{OptionRouter, r.Router.AsSlice()})
	}
	if len(r.DNS) > 0 {
		value := make([]byte, 0, len(r.DNS)*4)
		for _, address := range r.DNS {
			value = append(value, address.AsSlice()...)
		}
		options = append(options, option{OptionDNS, value})
	}
	if r.Hostname != "" {
		options = append(options, option{OptionHostname, []byte(r.Hostname)})
	}
	options = append(options, option{OptionEnd, nil})

	for _, item := range options {
		packet = append(packet, item.code, byte(len(item.value)))
		packet = append(packet, item.value...)
	}
	return packet
}

type option struct {
	code  byte
	value []byte
}

// ErrNoMessageType says a packet arrived without the one option every DHCP
// message must carry.
var ErrNoMessageType = errors.New("dhcp: packet has no message type")

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/userdata"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// Table attributes from Linux UAPI linux/netfilter/nf_tables.h.
const (
	nftaTableHandle   = 4
	nftaTableUserdata = 6
)

type trafficNetlink struct {
	conn   *nftables.Conn
	socket *netlink.Conn
}

func (r *trafficNetlink) close() {
	if r.conn != nil {
		_ = r.conn.CloseLasting()
	}
	r.conn = nil
	r.socket = nil
}
func (r *trafficNetlink) open() error {
	if r.conn != nil {
		return nil
	}
	c, err := nftables.New(nftables.AsLasting(), nftables.WithSockOptions(func(c *netlink.Conn) error { r.socket = c; return nil }))
	if err != nil {
		return err
	}
	r.conn = c
	return nil
}
func (r *trafficNetlink) identity() (uint64, string, error) {
	attrs, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: unix.NFTA_TABLE_NAME, Data: []byte("ngpanel_traffic\x00")}})
	if err != nil {
		return 0, "", err
	}
	messages, err := r.socket.Execute(netlink.Message{Header: netlink.Header{Type: netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | unix.NFT_MSG_GETTABLE), Flags: netlink.Request}, Data: append([]byte{unix.NFPROTO_INET, 0, 0, 0}, attrs...)})
	if err != nil {
		return 0, "", err
	}
	var handle uint64
	var comment string
	for _, message := range messages {
		if len(message.Data) < 4 {
			return 0, "", fmt.Errorf("short nft table message")
		}
		ad, err := netlink.NewAttributeDecoder(message.Data[4:])
		if err != nil {
			return 0, "", err
		}
		ad.ByteOrder = binary.BigEndian
		for ad.Next() {
			switch ad.Type() {
			case nftaTableHandle:
				handle = ad.Uint64()
			case nftaTableUserdata:
				comment, _ = userdata.GetString(ad.Bytes(), userdata.TypeComment)
			}
		}
		if err := ad.Err(); err != nil {
			return 0, "", err
		}
	}
	if handle == 0 || comment == "" {
		return 0, "", fmt.Errorf("accounting table identity missing")
	}
	return handle, comment, nil
}
func netlinkTrafficKey(name string, key []byte) (string, error) {
	if name == "upload" || name == "download" {
		if len(key) != 4 {
			return "", fmt.Errorf("invalid device key size")
		}
		return net.IP(key).String(), nil
	}
	// nft concatenations align every component to a 32-bit register.
	if len(key) != 20 {
		return "", fmt.Errorf("invalid flow key size %d", len(key))
	}
	network := ""
	switch key[16] {
	case 6:
		network = "tcp"
	case 17:
		network = "udp"
	default:
		return "", fmt.Errorf("invalid flow protocol")
	}
	tuple := trafficTuple(net.IP(key[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(key[4:6]))), net.IP(key[8:12]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(key[12:14]))), network)
	if tuple == "" {
		return "", fmt.Errorf("invalid flow tuple")
	}
	return "flow:" + tuple, nil
}

// Read only: no rule, set, counter, mark or route is changed here.
func (r *trafficNetlink) read(ctx context.Context, want, boot string, exits []trafficExit, flows bool) (counts map[string]trafficRaw, generation string, err error) {
	if err = r.open(); err != nil {
		return nil, "", err
	}
	defer func() {
		if err != nil {
			r.close()
		}
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = r.socket.SetDeadline(deadline); err != nil {
		return nil, "", err
	}
	handle, comment, err := r.identity()
	if err != nil {
		return nil, "", err
	}
	if comment != want {
		return nil, "", fmt.Errorf("accounting schema changed")
	}
	table := &nftables.Table{Name: "ngpanel_traffic", Family: nftables.TableFamilyINet}
	counts = map[string]trafficRaw{}
	sets := []string{"upload", "download"}
	if flows {
		sets = []string{"flow_upload", "flow_download"}
	}
	for _, name := range sets {
		elements, e := r.conn.GetSetElements(&nftables.Set{Table: table, Name: name})
		if e != nil {
			return nil, "", e
		}
		for _, element := range elements {
			if element.Counter == nil {
				return nil, "", fmt.Errorf("element counter missing")
			}
			key, e := netlinkTrafficKey(name, element.Key)
			if e != nil {
				return nil, "", e
			}
			v := counts[key]
			if name == "upload" || name == "flow_upload" {
				v.Up = element.Counter.Bytes
			} else {
				v.Down = element.Counter.Bytes
			}
			counts[key] = v
		}
	}
	if !flows {
		objects, e := r.conn.GetObjects(table)
		if e != nil {
			return nil, "", e
		}
		values := map[string]uint64{}
		for _, object := range objects {
			if counter, ok := object.(*nftables.CounterObj); ok {
				values[counter.Name] = counter.Bytes
			}
		}
		for _, exit := range exits {
			up, upOK := values[fmt.Sprintf("u_%x", exit.Mark)]
			down, downOK := values[fmt.Sprintf("d_%x", exit.Mark)]
			if !upOK || !downOK {
				return nil, "", fmt.Errorf("exit counter missing")
			}
			counts["exit:"+exit.Tag] = trafficRaw{Up: up, Down: down}
		}
	}
	after, comment, err := r.identity()
	if err != nil {
		return nil, "", err
	}
	if after != handle || comment != want {
		return nil, "", fmt.Errorf("accounting table replaced during read")
	}
	return counts, boot + "/nft/" + strconv.FormatUint(handle, 10), nil
}

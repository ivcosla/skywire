// +build !no_ci

package snet

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/skycoin/dmsg/cipher"
)

// ErrUnknownRemote returned for connection attempts for remotes
// missing from the translation table.
var ErrUnknownRemote = errors.New("unknown remote")

// TCPFactory implements Factory over TCP connection.
type TCPFactory struct {
	l   *net.TCPListener
	lpk cipher.PubKey
	pkt PubKeyTable
}

// NewTCPFactory constructs a new TCP Factory.
func NewTCPFactory(lpk cipher.PubKey, pkt PubKeyTable, l *net.TCPListener) *TCPFactory {
	return &TCPFactory{l, lpk, pkt}
}

// Accept accepts a remotely-initiated Transport.
func (f *TCPFactory) Accept(ctx context.Context) (*TCPTransport, error) {
	conn, err := f.l.AcceptTCP()
	if err != nil {
		return nil, err
	}

	raddr := conn.RemoteAddr().(*net.TCPAddr)
	rpk := f.pkt.RemotePK(raddr.String())
	if rpk.Null() {
		return nil, fmt.Errorf("error: %v, raddr: %v, rpk: %v", ErrUnknownRemote, raddr.String(), rpk)
	}

	// return &TCPTransport{conn, [2]cipher.PubKey{f.Pk, rpk}}, nil
	return &TCPTransport{conn, f.lpk, rpk}, nil
}

// Dial initiates a Transport with a remote node.
func (f *TCPFactory) Dial(ctx context.Context, remote cipher.PubKey) (*TCPTransport, error) {
	raddr := f.pkt.RemoteAddr(remote)
	if raddr == "" {
		return nil, ErrUnknownRemote
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", raddr)
	if err != nil {
		return nil, err
	}

	lsnAddr, err := net.ResolveTCPAddr("tcp", f.l.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("error in resolving local address")
	}
	locAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%v:%v", lsnAddr.IP.String(), "0"))
	if err != nil {
		return nil, fmt.Errorf("error in constructing local address ")
	}

	conn, err := net.DialTCP("tcp", locAddr, tcpAddr)
	if err != nil {
		return nil, err
	}

	return &TCPTransport{conn, f.lpk, remote}, nil
}

// Close implements io.Closer
func (f *TCPFactory) Close() error {
	if f == nil {
		return nil
	}
	return f.l.Close()
}

// Local returns the local public key.
func (f *TCPFactory) Local() cipher.PubKey {
	return f.lpk
}

// Type returns the Transport type.
func (f *TCPFactory) Type() string {
	return "tcp-transport"
}

// TCPTransport implements Transport over TCP connection.
type TCPTransport struct {
	*net.TCPConn
	localKey  cipher.PubKey
	remoteKey cipher.PubKey
}

// LocalPK returns the TCPTransport local public key.
func (tr *TCPTransport) LocalPK() cipher.PubKey {
	return tr.localKey
}

// RemotePK returns the TCPTransport remote public key.
func (tr *TCPTransport) RemotePK() cipher.PubKey {
	return tr.remoteKey
}

// Type returns the string representation of the transport type.
func (tr *TCPTransport) Type() string {
	return "tcp"
}

// PubKeyTable provides translation between remote PubKey and TCPAddr.
type PubKeyTable interface {
	RemoteAddr(remotePK cipher.PubKey) string
	RemotePK(address string) cipher.PubKey
	Count() int
}

type memPKTable struct {
	entries map[cipher.PubKey]string
	reverse map[string]cipher.PubKey
}

func memoryPubKeyTable(entries map[cipher.PubKey]string) *memPKTable {
	reverse := make(map[string]cipher.PubKey)
	for k, v := range entries {
		addr, err := net.ResolveTCPAddr("tcp", v)
		if err != nil {
			panic("error in resolving address")
		}
		reverse[addr.IP.String()] = k
	}
	return &memPKTable{entries, reverse}
}

// MemoryPubKeyTable returns in memory implementation of the PubKeyTable.
func MemoryPubKeyTable(entries map[cipher.PubKey]string) PubKeyTable {
	return memoryPubKeyTable(entries)
}

func (t *memPKTable) RemoteAddr(remotePK cipher.PubKey) string {
	return t.entries[remotePK]
}

func (t *memPKTable) RemotePK(address string) cipher.PubKey {
	addr, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		panic("net.ResolveTCPAddr")
	}
	return t.reverse[addr.IP.String()]
}

func (t *memPKTable) Count() int {
	return len(t.entries)
}

type filePKTable struct {
	dbFile string
	*memPKTable
}

// FilePubKeyTable returns file based implementation of the PubKeyTable.
func FilePubKeyTable(dbFile string) (PubKeyTable, error) {
	path, err := filepath.Abs(dbFile)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	entries := make(map[cipher.PubKey]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		components := strings.Fields(scanner.Text())
		if len(components) != 2 {
			continue
		}

		pk := cipher.PubKey{}
		if err := pk.UnmarshalText([]byte(components[0])); err != nil {
			continue
		}

		addr, err := net.ResolveTCPAddr("tcp", components[1])
		if err != nil {
			continue
		}

		entries[pk] = addr.String()
	}

	return &filePKTable{dbFile, memoryPubKeyTable(entries)}, nil
}
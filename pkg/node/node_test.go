package node

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"

	"github.com/skycoin/skycoin/src/util/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skycoin/skywire/pkg/cipher"

	"github.com/skycoin/skywire/internal/httpauth"
	"github.com/skycoin/skywire/pkg/app"
	"github.com/skycoin/skywire/pkg/messaging"
	"github.com/skycoin/skywire/pkg/messaging-discovery/client"
	"github.com/skycoin/skywire/pkg/transport"
)

func TestMain(m *testing.M) {
	lvl, _ := logging.LevelFromString("error") // nolint: errcheck
	logging.SetLevel(lvl)
	os.Exit(m.Run())
}

func TestNewNode(t *testing.T) {
	pk, sk := cipher.GenerateKeyPair()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(&httpauth.NextNonceResponse{Edge: pk, NextNonce: 1}) // nolint: errcheck
	}))
	defer srv.Close()

	conf := Config{Version: "1.0", LocalPath: "local", AppsPath: "apps"}
	conf.Node.StaticPubKey = pk
	conf.Node.StaticSecKey = sk
	conf.Messaging.Discovery = "http://skywire.skycoin.net:8001"
	conf.Messaging.ServerCount = 10
	conf.Transport.Discovery = srv.URL
	conf.Apps = []AppConfig{
		{App: "foo", Version: "1.1", Port: 1},
		{App: "bar", AutoStart: true, Port: 2},
	}

	defer os.RemoveAll("local")

	node, err := NewNode(&conf)
	require.NoError(t, err)

	assert.NotNil(t, node.router)
	assert.NotNil(t, node.appsConf)
	assert.NotNil(t, node.appsPath)
	assert.NotNil(t, node.localPath)
	assert.NotNil(t, node.startedApps)
}

func TestNodeStartClose(t *testing.T) {
	r := new(mockRouter)
	executer := &MockExecuter{}
	conf := []AppConfig{
		{App: "skychat", Version: "1.0", AutoStart: true, Port: 1},
		{App: "foo", Version: "1.0", AutoStart: false},
	}
	defer os.RemoveAll("skychat")
	node := &Node{config: &Config{}, router: r, executer: executer, appsConf: conf,
		startedApps: map[string]*appBind{}, logger: logging.MustGetLogger("test")}
	mConf := &messaging.Config{PubKey: cipher.PubKey{}, SecKey: cipher.SecKey{}, Discovery: client.NewMock()}
	node.messenger = messaging.NewClient(mConf)
	var err error

	tmConf := &transport.ManagerConfig{PubKey: cipher.PubKey{}, DiscoveryClient: transport.NewDiscoveryMock()}
	node.tm, err = transport.NewManager(tmConf, node.messenger)
	require.NoError(t, err)

	errCh := make(chan error)
	go func() {
		errCh <- node.Start()
	}()

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, node.Close())
	require.True(t, r.didClose)
	require.NoError(t, <-errCh)

	require.Len(t, executer.cmds, 1)
	assert.Equal(t, "skychat.v1.0", executer.cmds[0].Path)
	assert.Equal(t, "skychat/v1.0", executer.cmds[0].Dir)
}

func TestNodeSpawnApp(t *testing.T) {
	r := new(mockRouter)
	executer := &MockExecuter{}
	defer os.RemoveAll("skychat")
	apps := []AppConfig{{App: "skychat", Version: "1.0", AutoStart: false, Port: 10, Args: []string{"foo"}}}
	node := &Node{router: r, executer: executer, appsConf: apps, startedApps: map[string]*appBind{}, logger: logging.MustGetLogger("test")}

	require.NoError(t, node.StartApp("skychat"))
	time.Sleep(100 * time.Millisecond)

	require.NotNil(t, node.startedApps["skychat"])

	executer.Lock()
	require.Len(t, executer.cmds, 1)
	assert.Equal(t, "skychat.v1.0", executer.cmds[0].Path)
	assert.Equal(t, "skychat/v1.0", executer.cmds[0].Dir)
	assert.Equal(t, []string{"skychat.v1.0", "foo"}, executer.cmds[0].Args)
	executer.Unlock()

	ports := r.Ports()
	require.Len(t, ports, 1)
	assert.Equal(t, uint16(10), ports[0])

	require.NoError(t, node.StopApp("skychat"))
}

func TestNodeSpawnAppValidations(t *testing.T) {
	conn, _ := net.Pipe()
	r := new(mockRouter)
	executer := &MockExecuter{err: errors.New("foo")}
	defer os.RemoveAll("skychat")
	node := &Node{router: r, executer: executer,
		startedApps: map[string]*appBind{"skychat": {conn, 10}},
		logger:      logging.MustGetLogger("test")}

	cases := []struct {
		conf *AppConfig
		err  string
	}{
		{&AppConfig{App: "skychat", Version: "1.0", Port: 2}, "can't bind to reserved port 2"},
		{&AppConfig{App: "skychat", Version: "1.0", Port: 10}, "app skychat is already started"},
		{&AppConfig{App: "foo", Version: "1.0", Port: 11}, "failed to run app executable: foo"},
	}

	for _, c := range cases {
		t.Run(c.err, func(t *testing.T) {
			errCh := make(chan error)
			go func() {
				errCh <- node.SpawnApp(c.conf, nil)
			}()

			time.Sleep(100 * time.Millisecond)
			require.NoError(t, node.Close())
			err := <-errCh
			require.Error(t, err)
			assert.Equal(t, c.err, err.Error())
		})
	}
}

type MockExecuter struct {
	sync.Mutex
	err    error
	cmds   []*exec.Cmd
	stopCh chan struct{}
}

func (exc *MockExecuter) Start(cmd *exec.Cmd) (int, error) {
	exc.Lock()
	defer exc.Unlock()
	if exc.stopCh != nil {
		return -1, errors.New("already executing")
	}

	exc.stopCh = make(chan struct{})

	if exc.err != nil {
		return -1, exc.err
	}

	if exc.cmds == nil {
		exc.cmds = make([]*exec.Cmd, 0)
	}

	exc.cmds = append(exc.cmds, cmd)

	return 10, nil
}

func (exc *MockExecuter) Stop(pid int) error {
	exc.Lock()
	if exc.stopCh != nil {
		select {
		case <-exc.stopCh:
		default:
			close(exc.stopCh)
		}
	}
	exc.Unlock()
	return nil
}

func (exc *MockExecuter) Wait(cmd *exec.Cmd) error {
	<-exc.stopCh
	return nil
}

type mockRouter struct {
	sync.Mutex

	ports []uint16

	didStart bool
	didClose bool

	errChan chan error
}

func (r *mockRouter) Ports() []uint16 {
	r.Lock()
	p := r.ports
	r.Unlock()
	return p
}

func (r *mockRouter) Serve(_ context.Context) error {
	r.didStart = true
	return nil
}

func (r *mockRouter) ServeApp(conn net.Conn, port uint16, appConf *app.Config) error {
	r.Lock()
	if r.ports == nil {
		r.ports = []uint16{}
	}

	r.ports = append(r.ports, port)
	r.Unlock()

	if r.errChan == nil {
		r.Lock()
		r.errChan = make(chan error)
		r.Unlock()
	}

	return <-r.errChan
}

func (r *mockRouter) Close() error {
	r.didClose = true
	r.Lock()
	if r.errChan != nil {
		close(r.errChan)
	}
	r.Unlock()
	return nil
}

func (r *mockRouter) IsSetupTransport(tr transport.Transport) bool {
	return false
}

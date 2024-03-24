package authorizer

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/adsys/internal/testutils"
)

func TestIsAllowed(t *testing.T) {
	t.Parallel()

	bus := testutils.NewDbusConn(t)

	var emptyAction Action
	simpleAction := Action{
		ID: "simpleAction",
	}
	myUserOtherAction := Action{
		ID:      "UserOtherActionID",
		SelfID:  "Self",
		OtherID: "Other",
	}

	tests := map[string]struct {
		action    Action
		pid       int32
		uid       uint32
		actionUID uint32

		polkitAuthorize bool

		wantActionID    string
		wantAuthorized  bool
		wantPolkitError bool
	}{
		"Root is always authorized":             {uid: 0, wantAuthorized: true},
		"ActionAlwaysAllowed is always allowed": {action: ActionAlwaysAllowed, uid: 1000, wantAuthorized: true},
		"Valid process and ACK":                 {pid: 10000, uid: 1000, polkitAuthorize: true, wantAuthorized: true},
		"Valid process and NACK":                {pid: 10000, uid: 1000, polkitAuthorize: false, wantAuthorized: false},

		"User Action for own user translates to Self parameter as ID":   {action: myUserOtherAction, actionUID: 1000, pid: 10000, uid: 1000, wantActionID: myUserOtherAction.SelfID},
		"User Action on other user translates to Other parameter as ID": {action: myUserOtherAction, actionUID: 999, pid: 10000, uid: 1000, wantActionID: myUserOtherAction.OtherID},

		"Process doesn't exists":                         {pid: 99999, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: missing )":           {pid: 10001, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: ) at the end":        {pid: 10002, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: field isn't present": {pid: 10003, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: field isn't an int":  {pid: 10004, uid: 1000, polkitAuthorize: true, wantAuthorized: false},

		"Polkit dbus call errors out": {wantPolkitError: true, pid: 10000, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.action == emptyAction {
				tc.action = simpleAction
			}

			d := &DbusMock{
				IsAuthorized:    tc.polkitAuthorize,
				WantPolkitError: tc.wantPolkitError}
			a, err := New(bus, WithAuthority(d), WithRoot("testdata"))
			if err != nil {
				t.Fatalf("Failed to create authorizer: %v", err)
			}

			errAllowed := a.isAllowed(context.Background(), tc.action, tc.pid, tc.uid, tc.actionUID)

			if tc.wantActionID != "" {
				assert.Equal(t, tc.wantActionID, d.actionRequested.ID, "Unexpected action received by polkit")
			}

			assert.Equal(t, tc.wantAuthorized, errAllowed == nil, "isAllowed returned state match expectations")
		})
	}
}

func TestPeerCredsInfoAuthType(t *testing.T) {
	t.Parallel()

	p := peerCredsInfo{
		uid: 11111,
		pid: 22222,
	}
	assert.Equal(t, "uid: 11111, pid: 22222", p.AuthType(), "AuthType returns expected uid and pid")
}
func TestServerPeerCredsHandshake(t *testing.T) {
	t.Parallel()

	s := serverPeerCreds{}
	d, err := os.MkdirTemp("", "adsystest")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(d)

	socket := filepath.Join(d, "adsys.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Couldn't listen on socket: %v", err)
	}
	defer l.Close()

	wg := sync.WaitGroup{}
	wg.Add(1)
	var goroutineErr error
	go func() {
		defer wg.Done()
		unixAddr, err := net.ResolveUnixAddr("unix", socket)
		if err != nil {
			goroutineErr = fmt.Errorf("Couldn't resolve client socket address: %w", err)
			log.Print(goroutineErr)
			return
		}
		conn, err := net.DialUnix("unix", nil, unixAddr)
		if err != nil {
			goroutineErr = fmt.Errorf("Couldn't contact unix socket: %w", err)
			log.Print(goroutineErr)
			return
		}
		defer conn.Close()
	}()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Couldn't accept connexion from client: %v", err)
	}

	c, i, err := s.ServerHandshake(conn)
	if err != nil {
		t.Fatalf("Server handshake failed unexpectedly: %v", err)
	}
	if c == nil {
		t.Error("Received connexion is nil when we expected it not to")
	}

	user, err := user.Current()
	if err != nil {
		t.Fatalf("Couldn't retrieve current user: %v", err)
	}

	assert.Equal(t, fmt.Sprintf("uid: %s, pid: %d", user.Uid, os.Getpid()),
		i.AuthType(), "uid or pid received doesn't match what we expected")

	l.Close()
	wg.Wait()

	if goroutineErr != nil {
		t.Fatal(goroutineErr)
	}
}
func TestServerPeerCredsInvalidSocket(t *testing.T) {
	t.Parallel()

	s := serverPeerCreds{}
	_, _, _ = s.ServerHandshake(nil)
}

func TestMain(m *testing.M) {
	defer testutils.StartLocalSystemBus()()
	m.Run()
}

package control

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"time"
)

func dirOf(sock string) string { return filepath.Dir(sock) }

// Call sends req to the deck listening on sock and waits for its reply.
func Call(sock string, req Request, timeout time.Duration) (Reply, error) {
	c, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return Reply{}, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(timeout))
	data, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}
	if _, err := c.Write(append(data, '\n')); err != nil {
		return Reply{}, err
	}
	var r Reply
	err = json.NewDecoder(bufio.NewReader(c)).Decode(&r)
	return r, err
}

package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Transport struct {
	reader *bufio.Reader
	writer io.Writer
}

func NewTransport(in io.Reader, out io.Writer) *Transport {
	return &Transport{
		reader: bufio.NewReader(in),
		writer: out,
	}
}

// ReadRequest reads the next DAP request from the stream.
func (t *Transport) ReadRequest() (Request, error) {
	for {
		var cl int
		for {
			line, err := t.reader.ReadString('\n')
			if err != nil {
				return Request{}, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length: ") {
				cl, _ = strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
			}
		}
		
		body := make([]byte, cl)
		if _, err := io.ReadFull(t.reader, body); err != nil {
			return Request{}, err
		}

		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			return Request{}, err
		}
		return req, nil
	}
}

// SendResponse sends a DAP response.
func (t *Transport) SendResponse(resp Response) error {
	resp.Type = "response"
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(t.writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

// SendEvent sends a DAP event.
func (t *Transport) SendEvent(ev Event) error {
	ev.Type = "event"
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(t.writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}
